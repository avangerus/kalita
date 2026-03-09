Can't initialize prompt toolkit: No Windows console found. Are you running cmd.exe?
───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
Using openrouter/anthropic/claude-sonnet-4 model with API key from environment.
Aider v0.86.2
Main model: openrouter/anthropic/claude-sonnet-4 with diff edit format, infinite output
Weak model: openrouter/anthropic/claude-3-5-haiku
Git repo: .git with 26 files
Repo-map: using 4096 tokens, auto refresh
Here are summaries of some files present in my git repository.
Do not propose changes to these files, treat them as *read-only*.
If you need to edit any of these files, ask me to *add them to the chat* first.

internal\api\admin.go:
⋮
│type reloadReq struct {
│       DSLRoot   string `json:"dsl_root"`   // директория с *.dsl
│       EnumsRoot string `json:"enums_root"` // директория со справочниками enum
⋮
│func AdminReloadHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               var req reloadReq
│               if err := c.ShouldBindJSON(&req); err != nil && err != http.ErrBodyNotAllowed {
│                       c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
│                       return
│               }
│
│               dslRoot := strings.TrimSpace(req.DSLRoot)
│               if dslRoot == "" {
⋮

internal\api\blob.go:
⋮
│type BlobStore interface {
│       Put(key string, r io.Reader) (string, int64, string, error) // returns key, size, sha256
│       Delete(key string) error
│       Path(key string) (string, error) // local path (для local)
⋮
│type LocalBlobStore struct {
│       Root string // например, "./uploads"
⋮
│func (s *LocalBlobStore) ensureDir(p string) error {
│       return os.MkdirAll(p, 0o755)
⋮
│func randomHex(n int) string {
│       buf := make([]byte, n)
│       _, _ = rand.Read(buf)
│       return hex.EncodeToString(buf)
⋮

internal\api\files.go:
⋮
│func UploadFileHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│               field := c.Param("field")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
⋮
│func safeName(h *multipart.FileHeader) string {
│       name := h.Filename
│       name = filepath.Base(name)
│       name = strings.TrimSpace(name)
│       if name == "" {
│               return "file"
│       }
│       return name
⋮
│func DownloadAttachmentHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               id := c.Param("id")
│
│               storage.mu.RLock()
│               rec := storage.Data["core.Attachment"][id]
│               storage.mu.RUnlock()
│
│               if rec == nil || rec.Deleted {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Attachment not found"})
⋮

internal\api\handlers.go:
⋮
│func CreateHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               rawModule := c.Param("module")
│               rawEntity := c.Param("entity")
│
│               entity, ok := storage.NormalizeEntityName(rawModule, rawEntity)
│               if !ok {
│                       c.JSON(http.StatusBadRequest, gin.H{
│                               "errors": []FieldError{ferr(ErrTypeMismatch, "entity", "Entity not found")},
│                       })
⋮
│func ListHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
│               }
⋮
│func GetOneHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
⋮
│func UpdateHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
⋮
│func PatchHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
⋮
│func DeleteHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
│               }
│
⋮
│               type pendingNull struct {
│                       ent   string // FQN дочерней сущности
│                       id    string // id записи-ребёнка
│                       field string // имя поля-ссылки
│                       isArr bool   // массив ссылок
⋮
│func statusForErrors(errs []FieldError) int {
│       // 409, если есть конфликтные ошибки (unique/ref)
│       for _, e := range errs {
│               if e.Code == ErrUniqueViolation || e.Code == ErrRefNotFound {
│                       return http.StatusConflict
│               }
│       }
│       return http.StatusBadRequest
⋮
│func CountHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
│               }
│               schema := storage.Schemas[fqn]
│
⋮
│func RestoreHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               id := c.Param("id")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
⋮
│func BulkCreateHandler(storage *Storage) gin.HandlerFunc {
│       type bulkResult struct {
│               Data   map[string]any `json:"data,omitempty"`
│               Errors []FieldError   `json:"errors,omitempty"`
│       }
│
│       // системные поля, которые нельзя присылать на create
│       sys := map[string]struct{}{
│               "id": {}, "created_at": {}, "updated_at": {}, "version": {},
│       }
│
⋮
│func BulkPatchHandler(storage *Storage) gin.HandlerFunc {
│       type itemReq struct {
│               ID      string         `json:"id"`
│               Patch   map[string]any `json:"patch"`
│               IfMatch string         `json:"if_match"` // опционально; версия без кавычек
│       }
│       type itemRes struct {
│               ID     string         `json:"id"`
│               Status int            `json:"status"`
│               Data   map[string]any `json:"data,omitempty"`
⋮
│type filterCond struct {
│       field string
│       op    string // eq, in, gt, gte, lt, lte
│       vals  []string
⋮
│func buildConds(q url.Values) []filterCond {
│       var out []filterCond
│       for key, vals := range q {
│               switch key {
│               case "q", "offset", "limit", "sort", "order",
│                       "_offset", "_limit", "_sort", "_order",
│                       "nulls":
│                       continue
│               }
│               if len(vals) == 0 {
⋮
│func fieldTypeOf(schema *dsl.Entity, name string) string {
│       for _, f := range schema.Fields {
│               if f.Name == name {
│                       // нормализуем enum к "enum"
│                       if strings.HasPrefix(f.Type, "enum") || len(f.Enum) > 0 {
│                               return "enum"
│                       }
│                       return f.Type
│               }
│       }
⋮
│func compareByType(ft string, got any, op string, want string) bool {
│       // равенство/IN для всего — сравниваем строковые представления
│       toS := func(v any) string {
│               switch t := v.(type) {
│               case string:
│                       return t
│               default:
│                       return fmt.Sprint(t)
│               }
│       }
│
⋮
│func filterWithOps(all []*Record, schema *dsl.Entity, q url.Values) []*Record {
│       conds := buildConds(q)
│       if len(conds) == 0 && q.Get("q") == "" {
│               return all
│       }
│       out := make([]*Record, 0, len(all))
│       needle := strings.ToLower(strings.TrimSpace(q.Get("q")))
│
│loopRecs:
│       for _, r := range all {
⋮
│func BulkDeleteHandler(storage *Storage) gin.HandlerFunc {
│       type req struct {
│               IDs []string `json:"ids"`
│       }
│       type res struct {
│               ID     string       `json:"id,omitempty"`
│               Errors []FieldError `json:"errors,omitempty"`
│       }
│       return func(c *gin.Context) {
│               mod := c.Param("module")
⋮
│func BulkRestoreHandler(storage *Storage) gin.HandlerFunc {
│       type req struct {
│               IDs []string `json:"ids"`
│       }
│       type res struct {
│               ID     string       `json:"id,omitempty"`
│               Errors []FieldError `json:"errors,omitempty"`
│       }
│       return func(c *gin.Context) {
│               mod := c.Param("module")
⋮
│func readExpectedVersion(c *gin.Context, payload map[string]any) (int64, bool) {
│       // 1) If-Match: допускаем просто число (например, "3")
│       ifMatch := strings.TrimSpace(c.GetHeader("If-Match"))
│       if ifMatch != "" {
│               // уберём кавычки/weak-префикс вида W/"3"
│               if strings.HasPrefix(ifMatch, "W/") {
│                       ifMatch = strings.TrimPrefix(ifMatch, "W/")
│               }
│               ifMatch = strings.Trim(ifMatch, `"'`)
│               if v, err := strconv.ParseInt(ifMatch, 10, 64); err == nil {
⋮
│type ChildSpec struct {
│       ChildFQN string // "<module>.<Entity>"
│       FK       string // имя поля-ссылки в дочке
⋮
│func discoverChildren(
│       schemas map[string]*dsl.Entity,
│       parentFQN string,
│       expandAll bool,
│       expandSet map[string]bool,
⋮
│func equalFQN(a, b string) bool {
│       return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
⋮
│func batchByFK(storage *Storage, childFQN, fk string, parentIDs []string) []map[string]any {
│       res := make([]map[string]any, 0, 32)
│       parentSet := map[string]bool{}
│       for _, id := range parentIDs {
│               parentSet[id] = true
│       }
│
│       // In-memory (текущий MVP):
│       storage.mu.RLock()
│       defer storage.mu.RUnlock()
⋮
│func uniqStrings(in []string) []string {
│       seen := make(map[string]struct{}, len(in))
│       out := make([]string, 0, len(in))
│       for _, s := range in {
│               if _, ok := seen[s]; ok {
│                       continue
│               }
│               seen[s] = struct{}{}
│               out = append(out, s)
│       }
⋮
│func BatchGetHandler(storage *Storage) gin.HandlerFunc {
│       type req struct {
│               IDs []string `json:"ids"`
│       }
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
⋮

internal\api\helpers.go:
⋮
│func stringify(v interface{}) string {
│       switch t := v.(type) {
│       case string:
│               return t
│       case []byte:
│               return string(t)
│       default:
│               return strings.TrimSpace(fmtAny(v))
│       }
⋮
│func fmtAny(v interface{}) string {
│       return fmt.Sprintf("%v", v)
⋮

internal\api\meta.go:
⋮
│type metaEntityListItem struct {
│       Module string `json:"module"`
│       Entity string `json:"entity"`
⋮
│func MetaListHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               out := make([]metaEntityListItem, 0, len(storage.Schemas))
│               for fqn := range storage.Schemas {
│                       mod, ent := splitFQN(fqn)
│                       out = append(out, metaEntityListItem{Module: mod, Entity: ent})
│               }
│               c.JSON(http.StatusOK, out)
│       }
⋮
│type metaField struct {
│       Name     string            `json:"name"`
│       Type     string            `json:"type"`
│       ElemType string            `json:"elemType,omitempty"`
│       Ref      string            `json:"ref,omitempty"`
│       RefFQN   string            `json:"refFQN,omitempty"`
│       Enum     []string          `json:"enum,omitempty"`
│       Options  map[string]string `json:"options,omitempty"`
⋮
│type metaEntity struct {
│       Module      string         `json:"module"`
│       Entity      string         `json:"entity"`
│       Fields      []metaField    `json:"fields"`
│       Constraints map[string]any `json:"constraints,omitempty"` // {"unique":[["code"],["base","quote","
⋮
│func MetaEntityHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               mod := c.Param("module")
│               ent := c.Param("entity")
│
│               fqn, ok := storage.NormalizeEntityName(mod, ent)
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
│                       return
│               }
⋮
│func MetaCatalogHandler(storage *Storage) gin.HandlerFunc {
│       return func(c *gin.Context) {
│               name := c.Param("name")
│               dir, ok := storage.Enums[name]
│               if !ok {
│                       c.JSON(http.StatusNotFound, gin.H{"error": "Catalog not found"})
│                       return
│               }
│               c.JSON(http.StatusOK, gin.H{
│                       "name":  name,
⋮
│func splitFQN(fqn string) (string, string) {
│       i := strings.IndexByte(fqn, '.')
│       if i <= 0 || i >= len(fqn)-1 {
│               return "", fqn
│       }
│       return fqn[:i], fqn[i+1:]
⋮

internal\api\names.go:
⋮
│package api
│
⋮
│func (s *Storage) NormalizeEntityName(module, name string) (string, bool) {
│       if name == "" {
│               return "", false
│       }
│       ml := strings.ToLower(strings.TrimSpace(module))
│       nl := strings.ToLower(strings.TrimSpace(name))
│
│       // 1) есть модуль — ищем точное/регистронезависимое совпадение FQN
│       if ml != "" {
│               // сначала прямой ключ
⋮
│       var found string
⋮

internal\api\query.go:
⋮
│type SortKey struct {
│       Field string
│       Desc  bool
⋮
│type ListParams struct {
│       Limit   int
│       Offset  int
│       Sort    []SortKey
│       Filters map[string][]string
│       Q       string
│       Nulls   string // "last" (default) | "first"
⋮
│func parseListParams(q url.Values) ListParams {
│       // limit
│       limit := 50
│       lv := q.Get("_limit")
│       if lv == "" {
│               lv = q.Get("limit")
│       }
│       if lv != "" {
│               if n, err := strconv.Atoi(lv); err == nil && n >= 0 && n <= 1000 {
│                       limit = n
⋮
│func toString(v any) string {
│       switch t := v.(type) {
│       case string:
│               return t
│       case fmt.Stringer:
│               return t.String()
│       default:
│               return fmt.Sprintf("%v", v)
│       }
⋮
│func isNull(v any, ok bool) bool { return !ok || v == nil }
│
⋮
│func cmpByKey(a, b *Record, key string, nullsPolicy string, desc bool) int {
│       va, oka := a.Data[key]
│       vb, okb := b.Data[key]
│
│       na := isNull(va, oka)
│       nb := isNull(vb, okb)
│
│       // nulls first/last
│       if na && nb {
│               return 0
⋮
│func sortRecordsMultiNulls(records []*Record, keys []SortKey, nullsPolicy string) {
│       if len(keys) == 0 {
│               return
│       }
│       type kspec struct {
│               name string
│               desc bool
│       }
│       specs := make([]kspec, 0, len(keys))
│       for _, k := range keys {
⋮

internal\api\router.go:
⋮
│func RunServer(addr string, storage *Storage) {
│       // fail-fast, если есть критичные проблемы схемы
│       if issues := storage.SchemaLint(); len(issues) > 0 {
│               for _, it := range issues {
│                       log.Printf("[SCHEMA] %s.%s: %s (%s)\n", it.Entity, it.Field, it.Message, it.Code)
│               }
│               log.Fatal("schema has blocking issues; fix DSL and restart")
│       }
│       r := gin.Default()
│
⋮

internal\api\schema_lint.go:
⋮
│type SchemaIssue struct {
│       Entity  string `json:"entity"` // FQN: module.Entity
│       Field   string `json:"field"`
│       Code    string `json:"code"`
│       Message string `json:"message"`
⋮
│func (s *Storage) SchemaLint() []SchemaIssue {
│       var issues []SchemaIssue
│
│       for fqn, e := range s.Schemas {
│               for _, f := range e.Fields {
│                       // валидность on_delete
│                       if od := strings.TrimSpace(strings.ToLower(f.Options["on_delete"])); od != "" {
│                               switch od {
│                               case "restrict", "set_null", "cascade":
│                               default:
⋮

internal\api\storage.go:
⋮
│type Record struct {
│       ID        string                 `json:"id"`
│       Version   int64                  `json:"version"`
│       CreatedAt time.Time              `json:"created_at"`
│       UpdatedAt time.Time              `json:"updated_at"`
│       Deleted   bool                   `json:"-"`
│       Data      map[string]interface{} `json:"data"`
⋮
│type Storage struct {
│       mu      sync.RWMutex
│       Schemas map[string]*dsl.Entity             // FQN ("module.name") -> схема
│       Data    map[string]map[string]*Record      // FQN -> id -> запись
│       Enums   map[string]reference.EnumDirectory // каталог enum'ов (если нужен на валидации/UI)
│       entropy io.Reader
│       Blob    BlobStore
⋮
│func NewStorage(entities []*dsl.Entity, enumCatalog map[string]reference.EnumDirectory) *Storage {
│       src := rand.New(rand.NewSource(time.Now().UnixNano()))
│       s := &Storage{
│               Schemas: make(map[string]*dsl.Entity),
│               Data:    make(map[string]map[string]*Record),
│               Enums:   enumCatalog,
│               entropy: ulid.Monotonic(src, 0),
│       }
│       for _, e := range entities {
│               fqn := e.Module + "." + e.Name
⋮
│func (s *Storage) FindIncomingRefs(targetEntityFQN, targetID string) (refEntityFQN, refField string
│       s.mu.RLock()
│       defer s.mu.RUnlock()
│
│       for refFQN, schema := range s.Schemas {
│               records := s.Data[refFQN]
│               if records == nil {
│                       continue
│               }
│               for _, f := range schema.Fields {
⋮

internal\api\validation.go:
⋮
│type FieldError struct {
│       Code    string `json:"code"`
│       Field   string `json:"field"`
│       Message string `json:"message"`
⋮
│func ValidateAgainstSchema(
│       storage *Storage,
│       schema *dsl.Entity,
│       obj map[string]interface{},
│       idForUniqueExclusion string, // id текущей записи при обновлении (исключаем из unique-поиска)
│       entityKey string, // FQN сущности: "<module>.<name>"
⋮
│func coerceValue(storage *Storage, f dsl.Field, v interface{}) (interface{}, error) {
│       switch f.Type {
│       case "string":
│               return toStringStrict(v)
│       case "int":
│               return toIntStrict(v)
│       case "float":
│               return toFloatStrict(v)
│       case "bool":
│               return toBoolStrict(v)
⋮
│func toStringStrict(v interface{}) (string, error) {
│       switch t := v.(type) {
│       case string:
│               return t, nil
│       case float64: // json.Number по умолчанию в Go — float64
│               // не будем автоматически форматировать числа как строки — лучше отдать ошибку
│               return "", errors.New("must be string")
│       case bool:
│               return "", errors.New("must be string")
│       case nil:
⋮
│func toIntStrict(v interface{}) (int64, error) {
│       switch t := v.(type) {
│       case float64:
│               // JSON числа приходят как float64 — проверяем целостность
│               if t != float64(int64(t)) {
│                       return 0, errors.New("must be integer")
│               }
│               return int64(t), nil
│       case string:
│               n, err := strconv.ParseInt(t, 10, 64)
⋮
│func toFloatStrict(v interface{}) (float64, error) {
│       switch t := v.(type) {
│       case float64:
│               return t, nil
│       case string:
│               f, err := strconv.ParseFloat(t, 64)
│               if err != nil {
│                       return 0, errors.New("must be float")
│               }
│               return f, nil
⋮
│func toBoolStrict(v interface{}) (bool, error) {
│       switch t := v.(type) {
│       case bool:
│               return t, nil
│       case string:
│               switch strings.ToLower(strings.TrimSpace(t)) {
│               case "true", "1", "yes", "y", "on":
│                       return true, nil
│               case "false", "0", "no", "n", "off":
│                       return false, nil
⋮
│func ferr(code, field, msg string) FieldError {
│       return FieldError{Code: code, Field: field, Message: msg}
⋮
│func applyDefaults(schema *dsl.Entity, obj map[string]any) {
│       for _, f := range schema.Fields {
│               if f.Options == nil {
│                       continue
│               }
│               // не трогаем ссылки
│               if f.Type == "ref" || (f.Type == "array" && strings.EqualFold(f.ElemType, "ref")) {
│                       continue
│               }
│
⋮
│func checkReadonlyAndSystem(schema *dsl.Entity, obj map[string]any, isCreate bool) (errs []FieldErr
│       // системные поля
│       sys := []string{"id", "created_at", "updated_at", "version"}
│       for _, k := range sys {
│               if _, ok := obj[k]; ok {
│                       if k == "version" {
│                               // Разрешаем присутствие для If-Match-подобной логики, но не даём записать в Data
│                               delete(obj, k)
│                               continue
│                       }
⋮

internal\config\config.go:
⋮
│type Config struct {
│       Port        string `json:"port"`
│       DSLDir      string `json:"dslDir"`
│       EnumsDir    string `json:"enumsDir"`
│       DBURL       string `json:"dbUrl"`
│       AutoMigrate bool   `json:"autoMigrate"`
│
│       // Файлы (локально) и задел под S3
│       BlobDriver string `json:"blobDriver"` // "local" (default) | "s3"
│       FilesRoot  string `json:"filesRoot"`  // для local: папка хранения
│
⋮
│func def() Config {
│       return Config{
│               Port:        "8080",
│               DSLDir:      "dsl",
│               EnumsDir:    "reference/enums",
│               DBURL:       "",
│               AutoMigrate: false,
│
│               BlobDriver: "local",
│               FilesRoot:  "uploads",
│
⋮
│func loadJSON(path string) (Config, error) {
│       c := def()
│       b, err := os.ReadFile(path)
│       if err != nil {
│               return c, err
│       }
│       if err := json.Unmarshal(b, &c); err != nil {
│               return c, err
│       }
│       return c, nil
⋮
│func getenv(k, fallback string) string {
│       if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
│               return v
│       }
│       return fallback
│}
│func getenvBool(k string, fallback bool) bool {
│       if v, ok := os.LookupEnv(k); ok {
│               v = strings.TrimSpace(strings.ToLower(v))
│               if v == "1" || v == "true" || v == "yes" {
│                       return true
│               }
│               if v == "0" || v == "false" || v == "no" {
│                       return false
│               }
│       }
⋮
│func LoadWithPath(jsonPath string) Config {
│       cfg := def()
│
│       // JSON (если файл существует)
│       if st, err := os.Stat(jsonPath); err == nil && !st.IsDir() {
│               if c2, err := loadJSON(jsonPath); err == nil {
│                       cfg = c2
│               }
│       }
│
⋮

internal\dsl\model.go:
│package dsl
│
│type Entity struct {
│       Name        string
│       Module      string
│       Fields      []Field
│       Constraints Constraints
⋮
│type Constraints struct {
│       Unique [][]string `json:"unique,omitempty"` // наборы полей
⋮
│type Field struct {
│       Name      string            // имя поля
│       Type      string            // базовый тип: string,int,float,bool,date,datetime,enum,ref,array
│       Enum      []string          // значения enum, если Type == "enum"
│       RefTarget string            // целевая сущность для ref[...], если Type == "ref"
│       ElemType  string            // тип элемента для array[T], если Type == "array" (T без префикса arr
│       Options   map[string]string // required, unique, default...
⋮

internal\dsl\parser.go:
⋮
│func splitOptionTokens(s string) []string {
│       var out []string
│       var buf []rune
│       inSingle, inDouble := false, false
│       bracketDepth := 0 // внутри [ ... ] у регэкспа
│
│       flush := func() {
│               if len(buf) > 0 {
│                       out = append(out, string(buf))
│                       buf = buf[:0]
⋮
│func LoadEntities(path string) ([]*Entity, error) {
│       file, err := os.Open(path)
│       if err != nil {
│               return nil, err
│       }
│       defer file.Close()
│
│       var entities []*Entity
│       var current *Entity
│       currentModule := ""
⋮
│func LoadAllEntities(root string) (map[string]*Entity, error) {
│       result := make(map[string]*Entity)
│
│       err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
│               if walkErr != nil {
│                       return walkErr
│               }
│               if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".dsl") {
│                       return nil
│               }
│
⋮

internal\pg\apply.go:
│package pg
│
⋮
│func ApplyDDL(db *sql.DB, ddl map[string]string) error {
│       // стабильно: по имени сущности
│       keys := make([]string, 0, len(ddl))
│       for k := range ddl {
│               keys = append(keys, k)
│       }
│       sort.Strings(keys)
│
│       ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
│       defer cancel()
│
│       for _, k := range keys {
│               sqlText := strings.TrimSpace(ddl[k])
│               if sqlText == "" {
│                       continue
│               }
│               // [1.2] internal/pg/apply.go — игнорируем duplicate_object (42710)
│               if _, err := db.ExecContext(ctx, sqlText); err != nil {
│                       // pgx/stdlib возвращает *pgconn.PgError
│                       var pgErr *pgconn.PgError
│                       if errors.As(err, &pgErr) && pgErr.Code == "42710" {
│                               log.Printf("DDL skipped (already exists): %s (%s)", pgErr.ConstraintName, 
strings.TrimSpace(pgE
│                               continue
│                       }
│                       // подстраховка по фразе (на случай других объектов)
│                       e := strings.ToLower(err.Error())
│                       if strings.Contains(e, "already exists") || strings.Contains(e, "duplicate") {
⋮

internal\pg\conn.go:
│package pg
│
⋮
│func Open(url string) (*sql.DB, error) {
│       db, err := sql.Open("pgx", url)
│       if err != nil {
│               return nil, err
│       }
│       db.SetConnMaxLifetime(30 * time.Minute)
│       db.SetMaxOpenConns(10)
│       db.SetMaxIdleConns(5)
│
│       ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
⋮

internal\pg\schema.go:
⋮
│type OnDeletePolicy string
│
⋮
│func isReserved(s string) bool { _, ok := reserved[strings.ToLower(s)]; return ok }
│
⋮
│func safeSchema(module string) string { return strings.ToLower(module) }
│
│func safeTable(entity string) string {
│       t := plural(entity)
│       t = strings.ToLower(t)
│       if isReserved(t) {
│               // помечаем «опасное» имя префиксом
│               t = "e_" + t
│       }
│       return t
⋮
│func sqlIdent(s string) string { return `"` + strings.ToLower(s) + `"` }
│
⋮
│func onDeletePolicy(f dsl.Field) OnDeletePolicy {
│       if f.Options == nil {
│               return OnDeleteRestrict
│       }
│       switch strings.ToLower(strings.TrimSpace(f.Options["on_delete"])) {
│       case "set_null":
│               return OnDeleteSetNull
│       default:
│               return OnDeleteRestrict
│       }
⋮
│func GenerateDDL(entities map[string]*dsl.Entity) (map[string]string, error) {
│       out := make(map[string]string, len(entities)+2)
│
│       // стабильный порядок сущностей
│       keys := make([]string, 0, len(entities))
│       for k := range entities {
│               keys = append(keys, k)
│       }
│       sort.Strings(keys)
│
⋮
│       type fkStmt struct {
│               mod, tbl, idxName, col, refMod, refTbl string
│               onDelete                               OnDeletePolicy
⋮

internal\reference\leader.go:
⋮
│func LoadEnumCatalog(dir string) (map[string]EnumDirectory, error) {
│       result := make(map[string]EnumDirectory)
│       files, err := ioutil.ReadDir(dir)
│       if err != nil {
│               return nil, err
│       }
│       for _, file := range files {
│               if !file.IsDir() && (strings.HasSuffix(file.Name(), ".yaml") || strings.HasSuffix(file.Name(), ".
│                       path := filepath.Join(dir, file.Name())
│                       data, err := os.ReadFile(path)
⋮

internal\reference\model.go:
│package reference
│
⋮
│type EnumDirectory struct {
│       Name  string     `yaml:"name"`
│       Items []EnumItem `yaml:"items"`
⋮
│type EnumItem struct {
│       Code string `yaml:"code"`
│       Name string `yaml:"name"`
│       // Дополнительные поля: Order, Aliases, ValidFrom, ValidTo и т.д.
│       Order     int    `yaml:"order,omitempty"`
│       ValidFrom string `yaml:"valid_from,omitempty"`
│       ValidTo   string `yaml:"valid_to,omitempty"`
⋮

