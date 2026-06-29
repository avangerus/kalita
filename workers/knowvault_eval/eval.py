#!/usr/bin/env python3
"""KnowVault RAG eval harness — PHASE 0 blind retrieval benchmark.

Runs in two phases:
  1. GENERATE (once): samples random chunks from all kv_* collections,
     asks a judge LLM to form one natural question per chunk (gold = that chunk's file),
     generates off-domain trap questions, saves everything to a held-out JSON file.
  2. EVALUATE: embeds each question with the same model as the index,
     runs vector search, computes recall@k / MRR; scans the full index for noise
     chunks; checks hallucination rate on trap questions via the full answer pipeline.

The questions file is generated ONCE and re-used. The fix-iteration loop must not
read or modify that file — only the aggregate results file (timestamped JSON) is used
for before/after comparison.

Usage:
    python eval.py                      # gen (if needed) + measure
    python eval.py --regen              # force re-generate questions, then measure
    python eval.py --measure-only       # skip gen, run measurement on existing file

Env vars (all optional, shown with defaults):
    KV_QDRANT        http://192.168.1.4:6333
    KV_LM            http://localhost:1234
    KV_EMBED_MODEL   text-embedding-nomic-embed-text-v1.5
    KV_JUDGE_MODEL   qwen/qwen3.6-27b
    KV_CHAT_MODEL    openai/gpt-oss-20b

Zero external dependencies — stdlib only.
"""
import argparse
import json
import os
import random
import re
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone

# ── Config ─────────────────────────────────────────────────────────────────────
QDRANT      = os.environ.get("KV_QDRANT",      "http://192.168.1.4:6333")
LM          = os.environ.get("KV_LM",           "http://localhost:1234")
EMBED_MODEL = os.environ.get("KV_EMBED_MODEL",  "text-embedding-nomic-embed-text-v1.5")
JUDGE_MODEL = os.environ.get("KV_JUDGE_MODEL",  "qwen/qwen3.6-27b")
CHAT_MODEL  = os.environ.get("KV_CHAT_MODEL",   "openai/gpt-oss-20b")

SEED              = 42    # fixed for reproducibility
N_CORPUS          = 30    # questions generated from corpus chunks
N_TRAPS           = 10    # off-domain trap questions
DEFAULT_K         = 5     # recall@K

# Noise = a chunk carrying NO retrievable meaning: only table-drawing / separator
# glyphs and whitespace (borders, rule lines, empty cells), or a sub-threshold
# scattering of word characters. We measure WORD content, not raw alnum ratio, so a
# real table row ("Статус | Открыт | 12.05") is NOT punished — its cells are words.
# A chunk is noise iff it has fewer than this many "word tokens" of real content.
NOISE_MIN_WORD_TOKENS = 2
# Characters that are pure layout/formatting (never carry meaning on their own).
NOISE_FORMAT_CHARS = set("+-=|_~*#·•—–…:.,;!?/\\()[]{}<>\"'`“”«»\t \xa0")
# A "word token" for noise purposes: a run of 2+ letters/digits.
_WORD_TOKEN_RE = re.compile(r"[^\W_]{2,}", re.UNICODE)
# Degenerate generated questions (too short to be a real question) are rejected.
MIN_QUESTION_LEN = 12

_NOT_FOUND_RE = re.compile(
    r"(not (found|available|in (the )?context|mentioned|present)|"
    r"context does not (contain|include|mention|have)|"
    r"answer is not (in|from) the|"
    r"I (don'?t|cannot|can'?t) (find|see|locate|determine|answer)|"
    r"no (information|data|relevant|answer)|"
    r"nothing (found|relevant)|"
    r"не (найдено|содержится|указано|упомянуто)|"
    r"нет (информации|данных|в контексте|ответа)|"
    r"не могу (найти|определить|ответить)|"
    r"данный вопрос (не|выходит)|"
    r"за пределами (контекста|документ))",
    re.IGNORECASE,
)


# ── HTTP ───────────────────────────────────────────────────────────────────────
def http(url, body=None, method=None, timeout=120):
    data = json.dumps(body).encode() if body is not None else None
    meth = method or ("POST" if data is not None else "GET")
    req = urllib.request.Request(
        url, data=data, method=meth,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)


# ── Qdrant ─────────────────────────────────────────────────────────────────────
def list_kv_collections():
    r = http(f"{QDRANT}/collections")
    return sorted(
        c["name"]
        for c in r.get("result", {}).get("collections", [])
        if c["name"].startswith("kv_")
    )


def scroll_collection(collection, page=250):
    """Yield every point from a collection as a raw dict (no vectors)."""
    offset = None
    while True:
        body = {"limit": page, "with_payload": True, "with_vector": False}
        if offset is not None:
            body["offset"] = offset
        r = http(f"{QDRANT}/collections/{collection}/points/scroll", body=body)
        result = r.get("result", {})
        for pt in result.get("points", []):
            yield pt
        offset = result.get("next_page_offset")
        if not offset:
            break


def vector_search(collection, vec, top_k):
    r = http(
        f"{QDRANT}/collections/{collection}/points/search",
        body={"vector": vec, "limit": top_k, "with_payload": True},
    )
    return r.get("result", [])


# ── Embeddings ─────────────────────────────────────────────────────────────────
def embed_one(text, model):
    r = http(LM + "/v1/embeddings", {"model": model, "input": [text]})
    return r["data"][0]["embedding"]


# ── LLM judge ─────────────────────────────────────────────────────────────────
def judge(messages, max_tokens=300):
    r = http(
        LM + "/v1/chat/completions",
        {
            "model": JUDGE_MODEL,
            "messages": messages,
            "temperature": 0.05,
            "max_tokens": max_tokens,
        },
        timeout=240,
    )
    return r["choices"][0]["message"]["content"].strip()


def detect_lang(text):
    """Coarse script detector: 'ru' if Cyrillic dominates the letters, else 'en'.
    Question language must follow the source chunk's language."""
    cyr = sum(1 for c in text if "Ѐ" <= c <= "ӿ")
    lat = sum(1 for c in text if ("a" <= c.lower() <= "z"))
    return "ru" if cyr > lat else "en"


# Patterns that mark a question as META / self-referential — it asks about THIS
# fragment's structure ("in this excerpt", "tables listed", "column names",
# "данные в списке") instead of the document's content. Such questions break under
# re-chunking and are rejected.
_META_QUESTION_RE = re.compile(
    r"(this (excerpt|chunk|passage|fragment|text|table|list|snippet)|"
    r"the (excerpt|passage|fragment) (above|below)|"
    r"(column|field|table|row) names?|"
    r"data types?|"
    r"listed (in|here|above|below)|"
    r"в (этом|данном|приведённом|приведенном) (фрагменте|отрывке|тексте|списке|примере)|"
    r"в (списке|таблице|фрагменте|отрывке) (выше|ниже|указан|перечислен)|"
    r"какие (данные|поля|столбцы|колонки|таблицы) (указан|перечислен|приведен|есть))",
    re.IGNORECASE,
)


def is_degenerate_question(q):
    """Reject empty/too-short/meta questions so the held-out set stays clean."""
    if not q or len(q.strip()) < MIN_QUESTION_LEN:
        return True
    if "?" not in q:
        return True
    if _META_QUESTION_RE.search(q):
        return True
    return False


def make_corpus_question(chunk_text):
    """One natural question about the DOCUMENT's content (not this fragment's
    structure), phrased in the same language as the source chunk."""
    lang = detect_lang(chunk_text)
    lang_rule = (
        "Write the question in RUSSIAN." if lang == "ru"
        else "Write the question in ENGLISH."
    )
    return judge([{
        "role": "user",
        "content": (
            "You are building a retrieval evaluation dataset for document search.\n"
            "Read the excerpt and write EXACTLY ONE natural question a real user "
            "would ask the knowledge base, whose answer is found in this excerpt.\n"
            "\n"
            "HARD RULES:\n"
            "- The question must be about the DOCUMENT'S CONTENT (facts, procedures, "
            "definitions, values, responsibilities) — answerable from the source "
            "FILE regardless of how the text was split.\n"
            "- FORBIDDEN: questions about the fragment itself or its layout, e.g. "
            "\"what tables/columns are listed in this excerpt\", \"what data types "
            "are shown\", \"какие данные указаны в списке\", \"что перечислено в "
            "этом фрагменте\". Never reference \"this excerpt/list/table\".\n"
            "- Make it self-contained: a person who never saw this fragment should "
            "understand what is being asked.\n"
            f"- {lang_rule}\n"
            "\n"
            "Output ONLY the question text — no preamble, no quotes, no numbering.\n\n"
            f"Excerpt:\n{chunk_text[:1400]}"
        ),
    }])


def make_trap_questions(sample_texts, n):
    """N questions clearly outside the corpus domain."""
    samples = "\n---\n".join(t[:300] for t in sample_texts[:6])
    raw = judge(
        [{
            "role": "user",
            "content": (
                f"You are building a retrieval evaluation dataset.\n"
                f"The corpus below belongs to a specific domain. "
                f"Generate exactly {n} questions that CANNOT be answered by this corpus "
                f"because they are about completely different topics. "
                f"Output one question per line, no numbering, no explanations.\n\n"
                f"Corpus samples:\n{samples}\n\n"
                f"Generate {n} off-domain questions:"
            ),
        }],
        max_tokens=600,
    )
    lines = [ln.strip() for ln in raw.splitlines() if ln.strip()]
    # Fallback pool in case the LLM gives fewer lines
    _fallback = [
        "What is the boiling point of liquid nitrogen?",
        "Who invented the World Wide Web?",
        "What is the population of Brazil?",
        "How do you bake sourdough bread from scratch?",
        "In what year did the Berlin Wall fall?",
        "Explain the offside rule in football.",
        "What is the speed of light in a vacuum?",
        "Who wrote Crime and Punishment?",
        "How many bones are in the adult human body?",
        "What causes the aurora borealis?",
    ]
    idx = 0
    while len(lines) < n:
        candidate = _fallback[idx % len(_fallback)]
        if candidate not in lines:
            lines.append(candidate)
        idx += 1
    return lines[:n]


# ── Noise detection ────────────────────────────────────────────────────────────
def is_noise(text):
    """A chunk is noise when it carries no retrievable meaning — i.e. it is ONLY
    table-drawing/separator glyphs and whitespace, or has too few real word tokens
    to embed meaningfully.

    This deliberately does NOT punish legitimate table content: a row whose cells
    hold words/values ("Статус | Открыт | 12.05.2024") has word tokens and passes.
    What it flags is pure formatting — border rows ("+----+----+"), rule lines
    ("======="), pipe-and-dash skeletons, bullet-only lines, and empty/whitespace
    chunks. The measure is word-token count, so re-chunking that merges a table
    into one coherent block makes that block PASS, not fail."""
    if not text or not text.strip():
        return True
    # Strip everything that is pure layout — if nothing meaningful remains, it's noise.
    stripped = "".join(c for c in text if c not in NOISE_FORMAT_CHARS)
    if not stripped.strip():
        return True
    # Otherwise require a minimum of real word tokens (runs of 2+ letters/digits).
    return len(_WORD_TOKEN_RE.findall(text)) < NOISE_MIN_WORD_TOKENS


# ── Retrieval helper ───────────────────────────────────────────────────────────
def retrieve(question, collections, embed_model, k):
    """Return list[(score, collection, file)] sorted by score desc, length <= k."""
    vec = embed_one(question, embed_model)
    hits = []
    for col in collections:
        try:
            results = vector_search(col, vec, top_k=k)
            for r in results:
                hits.append((r["score"], col, r["payload"].get("file", "")))
        except urllib.error.HTTPError:
            continue
    hits.sort(key=lambda h: -h[0])
    return hits[:k]


def answer_question(question, collections, embed_model):
    """Replicate service.py answer_question — returns the LLM reply string."""
    vec = embed_one(question, embed_model)
    all_hits = []
    for col in collections:
        try:
            for r in vector_search(col, vec, top_k=4):
                all_hits.append((r["score"], r["payload"]))
        except urllib.error.HTTPError:
            continue
    all_hits.sort(key=lambda h: -h[0])
    top = [p for _, p in all_hits[:5]]
    if not top:
        return "Nothing found in indexed documents."
    context = "\n\n---\n\n".join(f"[{p['file']}]\n{p.get('text', '')}" for p in top)
    r = http(
        LM + "/v1/chat/completions",
        {
            "model": CHAT_MODEL,
            "messages": [
                {
                    "role": "system",
                    "content": (
                        "You are a company document search assistant. "
                        "Answer briefly and only based on the provided context. "
                        "If the answer is not in the context, say so."
                    ),
                },
                {"role": "user", "content": f"Context:\n{context}\n\nQuestion: {question}"},
            ],
            "temperature": 0.1,
        },
        timeout=240,
    )
    return r["choices"][0]["message"]["content"].strip()


# ── Phase 1: generate ──────────────────────────────────────────────────────────
def phase_generate(questions_file, embed_model):
    print(f"[gen] Listing kv_* collections in Qdrant ({QDRANT})...")
    collections = list_kv_collections()
    if not collections:
        sys.exit("[ERROR] No kv_* collections found. Is the index empty?")
    print(f"[gen] Collections: {collections}")

    print("[gen] Scrolling all chunks (this may take a moment)...")
    usable, total_pts, noise_pts = [], 0, 0
    for col in collections:
        for pt in scroll_collection(col):
            total_pts += 1
            pl = pt.get("payload", {})
            text = pl.get("text", "")
            if is_noise(text):
                noise_pts += 1
            else:
                usable.append({"collection": col, "file": pl.get("file", ""), "text": text})

    print(f"[gen] Points: total={total_pts}, usable={len(usable)}, noise={noise_pts}")
    if not usable:
        sys.exit("[ERROR] All chunks are noise-filtered. Nothing to generate questions from.")

    rng = random.Random(SEED)
    # Shuffle the whole usable pool (deterministic), then walk it: each candidate
    # gets up to MAX_TRIES generations; degenerate (empty/short/meta) results are
    # rejected and we move on — refilling from the pool until we reach N_CORPUS.
    # This guarantees no empty / self-referential questions in the held-out set.
    target = min(N_CORPUS, len(usable))
    pool = usable[:]
    rng.shuffle(pool)
    MAX_TRIES = 2

    print(f"[gen] Generating {target} corpus questions (judge: {JUDGE_MODEL})...")
    corpus_qs, rejected = [], 0
    for ch in pool:
        if len(corpus_qs) >= target:
            break
        tag = f"{ch['collection']}/{ch['file'][:40]}"
        q = None
        for _try in range(MAX_TRIES):
            try:
                cand = make_corpus_question(ch["text"])
            except Exception as exc:
                print(f"  [skip] {tag} -> gen error: {exc}")
                break
            if not is_degenerate_question(cand):
                q = cand.strip()
                break
            rejected += 1  # empty / too short / meta — retry this chunk
        if q is None:
            continue
        corpus_qs.append({
            "id": len(corpus_qs) + 1,
            "type": "corpus",
            "question": q,
            "lang": detect_lang(ch["text"]),
            "gold_collection": ch["collection"],
            "gold_file": ch["file"],
        })
        print(f"  [{len(corpus_qs):>2}/{target}] {tag}\n             -> {q[:80]}")

    print(f"[gen] Accepted {len(corpus_qs)}/{target} corpus questions "
          f"({rejected} degenerate generations rejected).")

    print(f"\n[gen] Generating {N_TRAPS} trap questions...")
    trap_texts = [c["text"] for c in rng.sample(usable, min(6, len(usable)))]
    try:
        trap_lines = make_trap_questions(trap_texts, N_TRAPS)
    except Exception as exc:
        print(f"[warn] Trap generation failed ({exc}); using fallback.")
        trap_lines = [
            "What is the boiling point of liquid nitrogen?",
            "Who invented the World Wide Web?",
            "What is the population of Brazil?",
            "How do you bake sourdough bread?",
            "In what year did the Berlin Wall fall?",
            "Explain the offside rule in football.",
            "What is the speed of light?",
            "Who wrote Crime and Punishment?",
            "How many bones are in the adult human body?",
            "What causes the aurora borealis?",
        ][:N_TRAPS]
    for tq in trap_lines:
        print(f"  trap: {tq}")
    traps = [{"id": 1000 + i, "type": "trap", "question": q}
             for i, q in enumerate(trap_lines)]

    payload = {
        "_note": (
            "HELD-OUT — do NOT read this file during fix iterations. "
            "Accept / reject fixes only from the aggregate metrics in the results file."
        ),
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "embed_model": embed_model,
        "seed": SEED,
        "collections": collections,
        "corpus_questions": corpus_qs,
        "traps": traps,
    }
    with open(questions_file, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
    print(f"\n[gen] Saved -> {questions_file}  "
          f"({len(corpus_qs)} corpus + {len(traps)} trap)")
    return payload


# ── Phase 2: measure ───────────────────────────────────────────────────────────
def phase_measure(qdata, embed_model, k):
    collections = qdata["collections"]
    corpus_qs   = qdata["corpus_questions"]
    traps       = qdata["traps"]

    # ── recall@k / MRR ────────────────────────────────────────────────────────
    print(f"\n[eval] recall@{k} / MRR on {len(corpus_qs)} corpus questions...")
    rr_list, hits_at_k, failed = [], 0, 0
    for i, item in enumerate(corpus_qs):
        q, gold_col, gold_file = item["question"], item["gold_collection"], item["gold_file"]
        print(f"  [{i+1:>2}/{len(corpus_qs)}] {q[:65]}", end="", flush=True)
        try:
            top = retrieve(q, collections, embed_model, k)
            rank = next(
                (pos for pos, (_, col, fn) in enumerate(top, 1)
                 if col == gold_col and fn == gold_file),
                None,
            )
            if rank is not None:
                hits_at_k += 1
                rr_list.append(1.0 / rank)
                print(f"  -> rank={rank}")
            else:
                rr_list.append(0.0)
                print(f"  -> MISS")
        except Exception as exc:
            failed += 1
            rr_list.append(0.0)
            print(f"  -> ERROR: {exc}")

    n_q = len(corpus_qs)
    recall_at_k = hits_at_k / n_q if n_q else 0.0
    mrr = sum(rr_list) / n_q if n_q else 0.0
    print(f"[eval] recall@{k}={recall_at_k:.3f}  MRR={mrr:.3f}  failed={failed}/{n_q}")

    # ── Noise chunk % ─────────────────────────────────────────────────────────
    print(f"\n[eval] Scanning all collections for noise chunks...")
    total_pts, noise_pts = 0, 0
    for col in collections:
        for pt in scroll_collection(col):
            total_pts += 1
            if is_noise(pt.get("payload", {}).get("text", "")):
                noise_pts += 1
    noise_pct = noise_pts / total_pts if total_pts else 0.0
    print(f"[eval] chunks: total={total_pts}, noise={noise_pts}, pct={noise_pct:.1%}")

    # ── Hallucination % on traps ──────────────────────────────────────────────
    print(f"\n[eval] Hallucination check on {len(traps)} trap questions...")
    hallucinations, trap_details = 0, []
    for i, trap in enumerate(traps):
        q = trap["question"]
        print(f"  [{i+1:>2}/{len(traps)}] {q[:65]}", end="", flush=True)
        try:
            ans = answer_question(q, collections, embed_model)
            honest = bool(_NOT_FOUND_RE.search(ans))
            if not honest:
                hallucinations += 1
            trap_details.append({"question": q, "answer": ans, "hallucinated": not honest})
            print(f"  -> {'honest' if honest else 'HALLUCINATED'}")
            print(f"          answer: {ans[:100]}")
        except Exception as exc:
            trap_details.append({"question": q, "answer": f"ERROR: {exc}", "hallucinated": False})
            print(f"  -> ERROR: {exc}")

    n_traps = len(traps)
    hallucination_pct = hallucinations / n_traps if n_traps else 0.0
    print(f"[eval] hallucinations={hallucinations}/{n_traps}  pct={hallucination_pct:.1%}")

    return {
        f"recall@{k}":         recall_at_k,
        "MRR":                 mrr,
        "noise_chunk_pct":     noise_pct,
        "hallucination_pct":   hallucination_pct,
        # detail fields for the results file
        "_detail": {
            "k":                       k,
            "corpus_questions_n":      n_q,
            "hits_at_k":               hits_at_k,
            "failed_retrievals":       failed,
            "total_chunks":            total_pts,
            "noise_chunks":            noise_pts,
            "hallucinations":          hallucinations,
            "n_traps":                 n_traps,
            "trap_details":            trap_details,
        },
    }


# ── Entry point ────────────────────────────────────────────────────────────────
def main():
    global JUDGE_MODEL
    ap = argparse.ArgumentParser(description="KnowVault RAG eval harness (Phase 0)")
    ap.add_argument("--questions-file", default="eval_questions.json",
                    help="Held-out question set (generated once, path relative to cwd)")
    ap.add_argument("--results-dir", default=".",
                    help="Directory for timestamped results JSON files")
    ap.add_argument("--embed-model", default=EMBED_MODEL,
                    help="Embedding model name — must match what was used to build the index")
    ap.add_argument("--judge-model", default=JUDGE_MODEL,
                    help="LLM used to generate questions and judge answers")
    ap.add_argument("--k", type=int, default=DEFAULT_K, help="k for recall@k (default 5)")
    ap.add_argument("--regen", action="store_true",
                    help="Force re-generate the question set even if the file exists")
    ap.add_argument("--measure-only", action="store_true",
                    help="Skip generation phase; measure against existing questions file")
    args = ap.parse_args()
    JUDGE_MODEL = args.judge_model

    print("=" * 62)
    print("KnowVault RAG eval harness")
    print(f"  Qdrant:        {QDRANT}")
    print(f"  LM Studio:     {LM}")
    print(f"  embed model:   {args.embed_model}")
    print(f"  judge model:   {JUDGE_MODEL}")
    print(f"  chat model:    {CHAT_MODEL}")
    print(f"  k:             {args.k}")
    print(f"  questions:     {args.questions_file}")
    print("=" * 62)

    # Connectivity check — fail fast with a clear message
    try:
        http(f"{QDRANT}/collections")
    except Exception as exc:
        sys.exit(
            f"\n[ERROR] Cannot reach Qdrant at {QDRANT}: {exc}\n"
            f"        Set KV_QDRANT env var if Qdrant is elsewhere.\n"
        )

    qfile = args.questions_file

    # ── Generation phase ───────────────────────────────────────────────────────
    if args.measure_only:
        if not os.path.exists(qfile):
            sys.exit(f"[ERROR] --measure-only but questions file not found: {qfile}")
        with open(qfile, encoding="utf-8") as f:
            qdata = json.load(f)
        print(f"[info] Loaded {len(qdata['corpus_questions'])} corpus + "
              f"{len(qdata['traps'])} trap questions from {qfile}")
    elif os.path.exists(qfile) and not args.regen:
        with open(qfile, encoding="utf-8") as f:
            qdata = json.load(f)
        print(f"[info] Re-using existing questions file: {qfile}")
        print(f"       corpus={len(qdata['corpus_questions'])}, traps={len(qdata['traps'])}")
        print(f"       (pass --regen to force re-generation)")
    else:
        qdata = phase_generate(qfile, args.embed_model)

    # ── Measurement phase ──────────────────────────────────────────────────────
    metrics = phase_measure(qdata, args.embed_model, args.k)
    detail  = metrics.pop("_detail")

    # ── Save results ───────────────────────────────────────────────────────────
    ts         = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    model_slug = args.embed_model.replace("/", "-").replace(".", "-")
    rfile      = os.path.join(args.results_dir, f"eval_results_{model_slug}_{ts}.json")
    os.makedirs(args.results_dir, exist_ok=True)

    report = {
        "run_at":         datetime.now(timezone.utc).isoformat(),
        "embed_model":    args.embed_model,
        "judge_model":    JUDGE_MODEL,
        "chat_model":     CHAT_MODEL,
        "qdrant":         QDRANT,
        "questions_file": qfile,
        "metrics":        metrics,
        "detail":         detail,
    }
    with open(rfile, "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)

    # ── Console report ─────────────────────────────────────────────────────────
    d = detail
    print("\n" + "=" * 62)
    print("BASELINE METRICS")
    print("=" * 62)
    print(f"  embed model:       {args.embed_model}")
    print(f"  recall@{args.k}:         {metrics[f'recall@{args.k}']:.3f}"
          f"  ({d['hits_at_k']}/{d['corpus_questions_n']} questions)")
    print(f"  MRR:               {metrics['MRR']:.3f}")
    print(f"  noise chunk %:     {metrics['noise_chunk_pct']:.1%}"
          f"  ({d['noise_chunks']}/{d['total_chunks']} chunks)")
    print(f"  hallucination %:   {metrics['hallucination_pct']:.1%}"
          f"  ({d['hallucinations']}/{d['n_traps']} trap questions)")
    if d["failed_retrievals"]:
        print(f"  [warn] retrieval errors: {d['failed_retrievals']}/{d['corpus_questions_n']}")
    print(f"\n  Results -> {rfile}")
    print("=" * 62)


if __name__ == "__main__":
    main()
