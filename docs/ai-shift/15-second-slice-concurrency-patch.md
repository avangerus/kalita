# Second slice concurrency patch

## What was fixed
- Fixed the in-memory correctness gap where concurrent `CreateWorkflowActionRequest` calls could create multiple pending requests for the same logical action request.
- The guarded duplicate case is the same `(entity, target_id, action, record_version)` being requested at the same time within one running server instance.

## How duplicates are prevented
- Request creation now derives a deterministic in-memory key from:
  - entity,
  - target record ID,
  - action,
  - record version.
- The runtime store keeps a small key-to-request-ID index beside the existing request map.
- After proposal validation succeeds, creation checks that key while holding the existing storage mutex:
  - if a request already exists for the key, the existing request is returned;
  - otherwise, the new request is inserted and the key is reserved.
- This keeps behavior deterministic without expanding the API or redesigning persistence.

## Residual risks
- The guard is runtime-memory only, so it does not survive process restart.
- The patch prevents duplicate request creation for the same logical key, but it does not redesign broader record/request concurrency semantics outside this slice.
- If future slices introduce request deletion or terminal-state cleanup, they will also need to define whether and when the in-memory key can be released.
