# Holonic Asset Quick Start

This guide runs the Core API and frontend locally for development. PostgreSQL runs in Docker with the database and credentials expected by the example Core API configuration.

## Prerequisites

- Go 1.26.5
- Node.js 24
- pnpm 11.5.0
- Docker with Docker Compose
- A Qiniu Kodo bucket and download domain for uploads and stored asset files
- API credentials for the configured image and LLM models. Image generation
  accepts any OpenAI-compatible endpoint, such as API Nebula.

The repository also supports [Lefthook](./lefthook-setup.md), but it is not required to start the application.

## 1. Clone the repository

```shell
git clone https://github.com/1024XEngineer/Holonic-Asset.git
cd Holonic-Asset
```

## 2. Start PostgreSQL

Start the repository's PostgreSQL service:

```shell
docker compose up -d --wait postgres
```

Docker creates the `holonic_asset` database, waits until PostgreSQL is healthy, and persists its data in a named volume. The included service already matches the database connection in the example Core API configuration. The Core API initializes its application tables and background-job schema when it first connects.

## 3. Configure the Core API

Copy the example configuration:

```shell
cp core-api/internal/config/config.example.yaml core-api/config.yaml
```

Review `config.yaml` and configure at least the following values:

- `auth.jwtSecret` with a deployment-specific secret of at least 32 bytes
- `image.models` and `image.defaultModel` for image generation
- `llm.models` and `llm.defaultModel` for structured generation tasks
- `qiniu.accessKey`, `qiniu.secretKey`, `qiniu.bucket`, and `qiniu.domain`

For image generation, set each model's `protocol`, `baseURL`, and `apiKey` in
`image.models`. The example uses API Nebula with the OpenAI-compatible
`chat_completions` protocol; replace it with any compatible endpoint and model
available from your account. Confirm each model identifier in your gateway's
documentation; the sample identifiers do not imply the gateway offers those
exact names. Enter API keys directly in your uncommitted configuration; YAML
values such as `${KEY}` are not automatically expanded from the environment.

For `openai_images` editing, set `image.models[].editFormat: multipart` when
the endpoint expects standard OpenAI file uploads. The default `json` keeps the
existing gateway edit encoding; the optional global `image.editFormat` supplies
the default for models that omit this setting. Video generation remains
configured separately under `video`.

Do not commit `config.yaml` or any credentials.

## 4. Start the Core API

From `core-api/`, run:

```shell
cd core-api
go run .
```

The API starts at `http://localhost:8080`. Its interactive API documentation is available at `http://localhost:8080/api/v1/docs`.

Keep the API running while completing the remaining steps.

## 5. Create a development login

Holonic Asset does not create users automatically. Generate a bcrypt hash for a development password, then apply the idempotent seed script.

Open a second terminal at the repository root. Use the Apache HTTP Server image to generate a bcrypt hash, then run the seed script with `psql` inside the PostgreSQL container:

```shell
export DEVELOPMENT_PASSWORD="choose-a-development-password"
export BOOTSTRAP_PASSWORD_HASH="$(docker run --rm httpd:2.4-alpine \
  htpasswd -bnBC 12 '' "$DEVELOPMENT_PASSWORD" | tr -d ':\n')"
docker compose exec -T postgres psql -U postgres -d holonic_asset \
  -v ON_ERROR_STOP=1 -v password_hash="$BOOTSTRAP_PASSWORD_HASH" \
  < core-api/scripts/seed.sql
unset BOOTSTRAP_PASSWORD_HASH DEVELOPMENT_PASSWORD
```

The script creates development accounts named `user1` through `user10`. Existing accounts are left unchanged. Sign in with one of those usernames and the password used to generate the hash.

## 6. Start the frontend

In the second terminal, start the frontend:

```shell
cd frontend
cp .env.example .env
pnpm install --frozen-lockfile
pnpm dev
```

Open `http://localhost:5173` and sign in with a seeded development account.

After stopping the frontend development server, return to the repository root and stop the database without deleting its data:

```shell
cd ..
docker compose stop postgres
```

## Verification

Before submitting changes, run the checks for the part of the project you changed.

Frontend:

```shell
cd frontend
pnpm format:check
pnpm lint
pnpm test
pnpm build
```

Core API:

```shell
cd core-api
go vet ./...
go test -race ./...
```

## Troubleshooting

### The Core API cannot connect to PostgreSQL

Run `docker compose ps postgres` from the repository root and confirm that the service is healthy. If port `5432` is already in use, stop the conflicting local database or container before starting the included service.

### The Core API reports invalid storage configuration

The API requires valid Qiniu credentials, a bucket name, and a download domain during startup. A bucket name is not a URL. Qiniu is used for object storage and uploads; it is independent from the image model endpoint.

### Login fails

The application has no self-service registration. Confirm that the seed script completed successfully and use the same password that produced `BOOTSTRAP_PASSWORD_HASH`.

### Asset generation fails

Confirm that every selected image or LLM model has a valid `baseURL`, `apiKey`, and supported protocol. Image endpoints must implement the configured OpenAI-compatible protocol. The model named by each `defaultModel` must exist in the corresponding `models` list.

For detailed Core API configuration, see the [Core API README](../../core-api/README.md).
