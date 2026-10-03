# Holonic Asset Core API

## Architecture

The full current-state design is documented in
[`docs/en/core-api-architecture.md`](../docs/en/core-api-architecture.md).

```text
database.go
log.go
task.go
internal/
  config/
  middleware/
  module/
    auth/
    generator/
      imageclient/
      llmclient/
      prompts/
      video_client/
    logger/
    processor/
      image/
      video/
    task/
    upload/
    viperx/
    workspace/
      asset/
      perspective/
      project/
  dto/
  handler/
  repository/
    dao/
  router/
```

Workspace is the business module for project and asset lifecycle operations. `internal/module/workspace/project` and `internal/module/workspace/asset` own their domain models, persistence ports, and managers; the root `workspace.Workspace` groups both capabilities. Repository implementations and DAOs remain infrastructure adapters under `internal/repository`.

Generator is a self-contained business module under `internal/module/generator`; it owns generation requests, run projections, task types, payloads, and task-handler skeletons. HTTP request and response contracts live in the independent `internal/dto` package. External image-provider capabilities remain under `internal/module/generator/imageclient`. `internal/module/processor` is a business-unaware media capability module: its image and video processors perform deterministic transformations without knowing Projects, Assets, task types, providers, or persistence. Shared helpers such as logging and Viper configuration loading live under `internal/module`. `internal/module/task` exposes one `task.Manager` entry point for task contracts, execution, queries, and transactional outbox dispatch.

Assets are aggregate documents. Asset metadata lives in the asset row, while nested content is stored in `asset_contents` and referenced by the asset's current `content_id`. Asset records map a version number to an immutable content snapshot; content edits use copy-on-write, records create a new snapshot, and rollback switches the current content pointer while discarding records newer than the target. Asset resources are not modeled as a separate table.

The Task module treats `Type` as an opaque string and `Payload` as opaque JSON. Business modules receive the task manager from the composition root and register their own type strings and handlers; Task never defines or switches on business task types. See `internal/module/task/README.md` for the task module usage guide.

## OpenAPI Contract

The Project, Generation, Upload, and Asset routes are OpenAPI-backed. Their Go
DTOs are the source of truth for runtime request binding, the OpenAPI 3.1
document, and generated frontend types. Successful responses consistently use
the `{code,message,data}` envelope.

With the API running, the contract and interactive documentation are available
at:

- `/api/v1/openapi.json`
- `/api/v1/openapi.yaml`
- `/api/v1/docs`

After changing an OpenAPI-backed DTO or route, regenerate the checked-in
contract and frontend types from `frontend/`:

```shell
pnpm api:generate
```

Run `pnpm api:check` to regenerate and type-check the frontend API surface.
Files under `frontend/src/model/generated/` must not be edited by hand.

## Authentication

Configure `auth.jwtSecret` and `auth.tokenExpiry` before starting the API. The
JWT secret must be replaced with a deployment-specific value of at least 32
bytes. `HOLONIC_AUTH_JWT_SECRET` overrides `auth.jwtSecret` from the YAML config
and is the preferred way to supply the secret in deployed environments. The
application only provides login and token verification; it does not create
initial users during startup.

To add the development users explicitly, generate a deployment-specific bcrypt
hash, export it as `BOOTSTRAP_PASSWORD_HASH`, and apply the SQL seed from the
`core-api` directory:

```shell
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 \
  -v password_hash="$BOOTSTRAP_PASSWORD_HASH" -f scripts/seed.sql
```

The seed is idempotent by username and does not replace existing passwords.

## Model Gateway Routing

Each model in `image`, `llm`, and `video` binds its own wire protocol and
endpoint settings (`baseURL` and `apiKey`). Image models may use any endpoint
implementing the supported OpenAI-compatible image protocols.
Top-level `baseURL` and `apiKey` settings remain available as optional fallback
defaults:

```yaml
image:
  defaultModel: "openai/gpt-image-2"
  fallbackModel: "google/gemini-3.1-flash-lite-image"
  editFormat: json
  models:
    - name: "openai/gpt-image-2"
      protocol: openai_images
      baseURL: "https://image-provider.example"
      apiKey: "..."
      editFormat: multipart
    - name: "google/gemini-3.1-flash-lite-image"
      protocol: chat_completions
      baseURL: "https://apinebula.ai"
      apiKey: "..."

llm:
  defaultModel: "google/gemini-3.7-flash"
  models:
    - name: "google/gemini-3.7-flash"
      protocol: chat_completions
      baseURL: "https://api.qnaigc.com"
      apiKey: "..."

video:
  models: []
  pollInterval: 5s
  pollTimeout: 45s
  maxRetries: 3
  retryDelay: 2s
```

Model identifiers in this example are illustrative. Confirm the model names
and capabilities exposed by your gateway, including API Nebula; different
gateways may use different identifiers. Enter the API key directly in your
uncommitted configuration. YAML values such as `${KEY}` are not automatically
expanded from environment variables.

`openai_images` calls `/v1/images/generations` or `/v1/images/edits`, while
`chat_completions` calls `/v1/chat/completions`. Any endpoint implementing one
of these OpenAI-compatible protocols can be configured, including API Nebula.
For `openai_images`, `image.models[].editFormat: multipart` uses the standard
OpenAI Images edit file-upload API. `json` preserves the existing gateway edit
format and is the default when no edit format is set. The optional global
`image.editFormat` applies to models without their own setting. This setting
does not affect `chat_completions` requests.

Generic image routes send the requested size unchanged and preserve edit masks
when the endpoint rejects them. Choose sizes supported by the selected model.
The deprecated `NewQNA...` constructors retain their old QNA-specific defaults,
minimum-size adjustment, and mask retry behavior for source compatibility.
Normal application configuration uses the generic routes.

Video retains its existing provider implementation. `fal_queue` derives video
task paths from the selected model, for example
`/queue/bytedance/seedance-2.0/image-to-video` and
`/queue/bytedance/seedance-2.0/requests`.

The image provider owns endpoint connections and model routing; protocol adapters
own request and response formats. The order of `models` does not select a
default or control fallback. Each image or LLM `defaultModel` must name an entry
in its client's `models` array. Video has no configured default model: an
explicit request model is required when video `models` are configured. With an
empty video `models` array, the existing fixed Fal Queue paths remain active.
For images, `fallbackModel` is tried only after a transient primary failure. The
legacy singular image `provider` setting remains supported only when no image
`models` array is configured.

Qiniu settings below configure object storage for uploads and stored asset
references; they do not select an image model.

## Qiniu Uploads

Configure `qiniu.accessKey`, `qiniu.secretKey`, `qiniu.bucket`, and
`qiniu.domain` in the selected YAML config. `bucket` is the Kodo bucket/S3
bucket name, not a URL. `domain` may be a Kodo download domain or a virtual-host
S3 endpoint such as `https://bucket.s3.cn-east-1.qiniucs.com`.
`qiniu.uploadURL` defaults to `https://upload.qiniup.com`, the upload token
defaults to one hour, and private download URLs default to 30 minutes.

`POST /api/v1/uploads` accepts `contentType` and `contentLength`. It returns a
server-generated object key, temporary private object URL, Qiniu upload
endpoint, and a short-lived upload token. Upload the file to `uploadURL` as
multipart form data using `uploadToken` as `token`, `objectKey` as `key`, and
the file as `file`. The token only permits that object key, MIME type, and exact
size. Project and asset data persist object keys; API responses resolve those
keys to temporary URLs without changing their JSON fields.
