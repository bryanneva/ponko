#!/usr/bin/env bash
set -euo pipefail

# Ponko Setup Script
# Usage:
#   ./scripts/setup.sh           Interactive first-run setup
#   ./scripts/setup.sh deploy    Re-deploy from existing ponko.yaml
#   ./scripts/setup.sh validate  Check config + health

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CONFIG_FILE="$PROJECT_DIR/ponko.yaml"
EXAMPLE_FILE="$PROJECT_DIR/ponko.example.yaml"

# ── Helpers ───────────────────────────────────────────

info()  { printf "\033[1;34m▸\033[0m %s\n" "$1"; }
ok()    { printf "\033[1;32m✓\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33m⚠\033[0m %s\n" "$1"; }
err()   { printf "\033[1;31m✗\033[0m %s\n" "$1" >&2; }
die()   { err "$1"; exit 1; }

# Read a value from ponko.yaml (flat key: value format, all keys globally unique)
yaml_get() {
    local key="$1"
    grep -E "^${key}:" "$CONFIG_FILE" 2>/dev/null \
        | head -1 \
        | sed 's/^[^:]*:[[:space:]]*//' \
        | sed 's/^"//' \
        | sed 's/"$//' \
        | sed "s/^'//" \
        | sed "s/'$//"
}

# Write a value to ponko.yaml
yaml_set() {
    local key="$1"
    local value="$2"
    if grep -qE "^${key}:" "$CONFIG_FILE" 2>/dev/null; then
        sed -i '' "s|^\(${key}:\).*|\1 \"${value}\"|" "$CONFIG_FILE"
    fi
}

# Prompt for a value with optional default
prompt_value() {
    local label="$1"
    local default="${2:-}"
    local value

    if [ -n "$default" ]; then
        printf "\033[1m%s\033[0m [%s]: " "$label" "$default"
    else
        printf "\033[1m%s\033[0m: " "$label"
    fi
    read -r value
    if [ -z "$value" ] && [ -n "$default" ]; then
        value="$default"
    fi
    echo "$value"
}

# Prompt yes/no
prompt_yn() {
    local label="$1"
    local default="${2:-n}"
    local hint="y/N"
    [ "$default" = "y" ] && hint="Y/n"

    printf "\033[1m%s\033[0m [%s]: " "$label" "$hint"
    read -r answer
    answer="${answer:-$default}"
    [[ "$answer" =~ ^[Yy] ]]
}

# Load all config values into variables. Called by both deploy paths.
load_config() {
    CFG_BOT_NAME=$(yaml_get "bot_name")
    CFG_TIMEZONE=$(yaml_get "bot_timezone")
    CFG_SYSTEM_PROMPT=$(yaml_get "bot_system_prompt")
    CFG_BOT_TOKEN=$(yaml_get "slack_bot_token")
    CFG_SIGNING_SECRET=$(yaml_get "slack_signing_secret")
    CFG_BOT_USER_ID=$(yaml_get "slack_bot_user_id")
    CFG_ANTHROPIC_KEY=$(yaml_get "anthropic_api_key")
    CFG_PLATFORM=$(yaml_get "deploy_platform")
    CFG_APP_NAME=$(yaml_get "deploy_app_name")
    CFG_REGION=$(yaml_get "deploy_region")
    CFG_URL=$(yaml_get "deploy_url")
    CFG_API_KEY=$(yaml_get "deploy_api_key")
    CFG_COOKIE_KEY=$(yaml_get "deploy_cookie_signing_key")
    CFG_WORKER_CONCURRENCY=$(yaml_get "deploy_worker_concurrency")
    CFG_DASHBOARD_CLIENT_ID=$(yaml_get "dashboard_slack_client_id")
    CFG_DASHBOARD_CLIENT_SECRET=$(yaml_get "dashboard_slack_client_secret")
    CFG_DASHBOARD_TEAM_ID=$(yaml_get "dashboard_slack_team_id")
    CFG_MCP_URLS=$(yaml_get "mcp_server_urls")
    CFG_MCP_KEY=$(yaml_get "mcp_access_key")
    CFG_GITHUB_URL=$(yaml_get "github_mcp_url")
    CFG_GITHUB_PAT=$(yaml_get "github_pat")
    CFG_LINEAR_URL=$(yaml_get "linear_mcp_url")
    CFG_LINEAR_TOKEN=$(yaml_get "linear_access_token")
    CFG_LINEAR_TOKEN_URL=$(yaml_get "linear_token_url")
    CFG_LINEAR_CLIENT_ID=$(yaml_get "linear_client_id")
    CFG_LINEAR_CLIENT_SECRET=$(yaml_get "linear_client_secret")
    CFG_LINEAR_REFRESH=$(yaml_get "linear_refresh_token")
    CFG_OTEL_ENDPOINT=$(yaml_get "otel_endpoint")
    CFG_OTEL_EXPORTER=$(yaml_get "otel_exporter")
}

# Add a secret to the secrets array if the value is non-empty.
# Usage: add_secret "ENV_VAR" "$value"
add_secret() {
    local env_var="$1"
    local value="$2"
    if [ -n "$value" ]; then
        SECRETS+=("${env_var}=${value}")
    fi
}

# Add a line to the .env file if the value is non-empty.
# Usage: add_env "ENV_VAR" "$value"
add_env() {
    local env_var="$1"
    local value="$2"
    if [ -n "$value" ]; then
        echo "${env_var}=${value}" >> "$ENV_FILE"
    fi
}

# ── Prerequisites ─────────────────────────────────────

check_prerequisites() {
    local missing=0

    if command -v fly >/dev/null 2>&1; then
        ok "fly CLI found"
    else
        err "fly CLI not found"
        info "Install: curl -L https://fly.io/install.sh | sh"
        missing=1
    fi

    if command -v openssl >/dev/null 2>&1; then
        ok "openssl found"
    else
        err "openssl not found (needed for secret generation)"
        missing=1
    fi

    if [ $missing -ne 0 ]; then
        die "Install missing prerequisites and re-run"
    fi
}

check_fly_auth() {
    if fly auth whoami >/dev/null 2>&1; then
        ok "Logged in to Fly.io as $(fly auth whoami 2>/dev/null)"
    else
        info "Not logged in to Fly.io"
        fly auth login
    fi
}

# ── Interactive Setup ─────────────────────────────────

interactive_setup() {
    echo ""
    echo "╔══════════════════════════════════════╗"
    echo "║         Ponko Setup Wizard           ║"
    echo "╚══════════════════════════════════════╝"
    echo ""

    check_prerequisites

    # Platform selection
    echo ""
    info "Choose deployment platform:"
    echo "  1) Fly.io (recommended)"
    echo "  2) Docker (local/self-hosted)"
    local platform_choice
    platform_choice=$(prompt_value "Platform" "1")
    local platform="fly"
    [ "$platform_choice" = "2" ] && platform="docker"

    if [ "$platform" = "fly" ]; then
        check_fly_auth
    fi

    # Copy example config
    if [ ! -f "$CONFIG_FILE" ]; then
        cp "$EXAMPLE_FILE" "$CONFIG_FILE"
        ok "Created ponko.yaml from template"
    else
        warn "ponko.yaml already exists — updating values in place"
    fi

    yaml_set "deploy_platform" "$platform"

    # Slack setup
    echo ""
    info "Step 1: Slack App"
    echo "  You need a Slack app with bot token scopes."
    echo "  See: docs/slack-setup.md"
    echo ""
    if prompt_yn "Open Slack app creation page in browser?"; then
        open "https://api.slack.com/apps" 2>/dev/null || true
    fi

    echo ""
    local bot_token
    bot_token=$(prompt_value "Slack Bot Token (xoxb-...)")
    [ -z "$bot_token" ] && die "Bot token is required"
    yaml_set "slack_bot_token" "$bot_token"

    local signing_secret
    signing_secret=$(prompt_value "Slack Signing Secret")
    [ -z "$signing_secret" ] && die "Signing secret is required"
    yaml_set "slack_signing_secret" "$signing_secret"

    local bot_user_id
    bot_user_id=$(prompt_value "Slack Bot User ID (U...)")
    [ -z "$bot_user_id" ] && die "Bot user ID is required"
    yaml_set "slack_bot_user_id" "$bot_user_id"

    # Anthropic
    echo ""
    info "Step 2: AI"
    local anthropic_key
    anthropic_key=$(prompt_value "Anthropic API Key (sk-ant-...)")
    [ -z "$anthropic_key" ] && die "Anthropic API key is required"
    yaml_set "anthropic_api_key" "$anthropic_key"

    # Bot config
    echo ""
    info "Step 3: Bot settings"
    local bot_name
    bot_name=$(prompt_value "Bot name" "Ponko")
    yaml_set "bot_name" "$bot_name"

    local timezone
    timezone=$(prompt_value "Timezone" "America/Los_Angeles")
    yaml_set "bot_timezone" "$timezone"

    local system_prompt
    system_prompt=$(prompt_value "System prompt (blank for default)" "")
    if [ -n "$system_prompt" ]; then
        yaml_set "bot_system_prompt" "$system_prompt"
    fi

    # Auto-generate secrets
    local api_key
    api_key=$(openssl rand -hex 32)
    yaml_set "deploy_api_key" "$api_key"
    ok "Generated API key"

    local cookie_key
    cookie_key=$(openssl rand -hex 32)
    yaml_set "deploy_cookie_signing_key" "$cookie_key"
    ok "Generated cookie signing key"

    # Optional: Dashboard OAuth
    echo ""
    if prompt_yn "Set up dashboard OAuth? (allows Slack login to admin UI)"; then
        local client_id
        client_id=$(prompt_value "Slack Client ID")
        yaml_set "dashboard_slack_client_id" "$client_id"

        local client_secret
        client_secret=$(prompt_value "Slack Client Secret")
        yaml_set "dashboard_slack_client_secret" "$client_secret"

        local team_id
        team_id=$(prompt_value "Slack Team ID (T...)")
        yaml_set "dashboard_slack_team_id" "$team_id"
    fi

    # Optional: MCP
    echo ""
    if prompt_yn "Configure MCP tool servers?"; then
        local mcp_urls
        mcp_urls=$(prompt_value "MCP Server URLs (comma-separated)")
        yaml_set "mcp_server_urls" "$mcp_urls"

        local mcp_key
        mcp_key=$(prompt_value "MCP Access Key (blank if none)" "")
        if [ -n "$mcp_key" ]; then
            yaml_set "mcp_access_key" "$mcp_key"
        fi
    fi

    # Optional: GitHub MCP
    echo ""
    if prompt_yn "Configure GitHub MCP integration?"; then
        local github_url
        github_url=$(prompt_value "GitHub MCP server URL")
        yaml_set "github_mcp_url" "$github_url"

        local github_pat
        github_pat=$(prompt_value "GitHub Personal Access Token")
        yaml_set "github_pat" "$github_pat"
    fi

    # Optional: Linear MCP
    echo ""
    if prompt_yn "Configure Linear MCP integration?"; then
        local linear_url
        linear_url=$(prompt_value "Linear MCP server URL")
        yaml_set "linear_mcp_url" "$linear_url"

        local linear_token
        linear_token=$(prompt_value "Linear access token")
        yaml_set "linear_access_token" "$linear_token"

        local linear_token_url
        linear_token_url=$(prompt_value "Linear OAuth token URL")
        yaml_set "linear_token_url" "$linear_token_url"

        local linear_client_id
        linear_client_id=$(prompt_value "Linear OAuth client ID")
        yaml_set "linear_client_id" "$linear_client_id"

        local linear_client_secret
        linear_client_secret=$(prompt_value "Linear OAuth client secret")
        yaml_set "linear_client_secret" "$linear_client_secret"

        local linear_refresh
        linear_refresh=$(prompt_value "Linear OAuth refresh token")
        yaml_set "linear_refresh_token" "$linear_refresh"
    fi

    ok "Config saved to ponko.yaml"

    # Deploy
    echo ""
    if [ "$platform" = "fly" ]; then
        deploy_fly
    else
        deploy_docker
    fi
}

# ── Fly.io Deploy ─────────────────────────────────────

deploy_fly() {
    info "Deploying to Fly.io..."

    load_config

    local app_name="$CFG_APP_NAME"
    if [ -z "$app_name" ]; then
        local random_suffix
        random_suffix=$(openssl rand -hex 2)
        app_name=$(prompt_value "App name" "ponko-${random_suffix}")
        yaml_set "deploy_app_name" "$app_name"
    fi

    local region="${CFG_REGION:-iad}"

    local dashboard_url="$CFG_URL"
    if [ -z "$dashboard_url" ]; then
        dashboard_url="https://${app_name}.fly.dev"
        yaml_set "deploy_url" "$dashboard_url"
    fi

    # Cache fly apps list (one API call instead of two)
    local fly_apps
    fly_apps=$(fly apps list 2>/dev/null || true)

    # Create app
    info "Creating Fly.io app: $app_name"
    if echo "$fly_apps" | grep -q "$app_name"; then
        warn "App $app_name already exists, skipping creation"
    else
        fly apps create "$app_name" --machines
        ok "App created"
    fi

    # Create and attach Postgres
    local db_name="${app_name}-db"
    info "Creating Postgres: $db_name"
    if echo "$fly_apps" | grep -q "$db_name"; then
        warn "Database $db_name already exists, skipping creation"
    else
        fly postgres create --name "$db_name" --region "$region" --initial-cluster-size 1 --vm-size shared-cpu-1x --volume-size 1
        ok "Postgres created"
    fi

    info "Attaching database..."
    fly postgres attach "$db_name" --app "$app_name" 2>/dev/null || warn "Database may already be attached"

    # Build secrets array (no eval needed)
    info "Setting secrets..."
    local SECRETS=()
    add_secret "ANTHROPIC_API_KEY" "$CFG_ANTHROPIC_KEY"
    add_secret "SLACK_BOT_TOKEN" "$CFG_BOT_TOKEN"
    add_secret "SLACK_SIGNING_SECRET" "$CFG_SIGNING_SECRET"
    add_secret "SLACK_BOT_USER_ID" "$CFG_BOT_USER_ID"
    add_secret "PONKO_API_KEY" "$CFG_API_KEY"
    add_secret "COOKIE_SIGNING_KEY" "$CFG_COOKIE_KEY"
    add_secret "DASHBOARD_URL" "$dashboard_url"

    [ -n "$CFG_BOT_NAME" ] && [ "$CFG_BOT_NAME" != "Ponko" ] && add_secret "BOT_NAME" "$CFG_BOT_NAME"
    [ -n "$CFG_TIMEZONE" ] && [ "$CFG_TIMEZONE" != "America/Los_Angeles" ] && add_secret "TIMEZONE" "$CFG_TIMEZONE"
    add_secret "WORKER_CONCURRENCY" "$CFG_WORKER_CONCURRENCY"
    add_secret "SYSTEM_PROMPT" "$CFG_SYSTEM_PROMPT"

    # Dashboard OAuth
    add_secret "SLACK_CLIENT_ID" "$CFG_DASHBOARD_CLIENT_ID"
    add_secret "SLACK_CLIENT_SECRET" "$CFG_DASHBOARD_CLIENT_SECRET"
    add_secret "SLACK_TEAM_ID" "$CFG_DASHBOARD_TEAM_ID"

    # MCP
    [ "$CFG_MCP_URLS" != "[]" ] && add_secret "MCP_SERVER_URLS" "$CFG_MCP_URLS"
    add_secret "MCP_ACCESS_KEY" "$CFG_MCP_KEY"

    # GitHub MCP
    add_secret "GITHUB_MCP_URL" "$CFG_GITHUB_URL"
    add_secret "GITHUB_PAT" "$CFG_GITHUB_PAT"

    # Linear MCP
    add_secret "LINEAR_MCP_URL" "$CFG_LINEAR_URL"
    add_secret "LINEAR_MCP_ACCESS_TOKEN" "$CFG_LINEAR_TOKEN"
    add_secret "LINEAR_MCP_TOKEN_URL" "$CFG_LINEAR_TOKEN_URL"
    add_secret "LINEAR_MCP_CLIENT_ID" "$CFG_LINEAR_CLIENT_ID"
    add_secret "LINEAR_MCP_CLIENT_SECRET" "$CFG_LINEAR_CLIENT_SECRET"
    add_secret "LINEAR_MCP_REFRESH_TOKEN" "$CFG_LINEAR_REFRESH"

    # Observability
    add_secret "OTEL_EXPORTER_OTLP_ENDPOINT" "$CFG_OTEL_ENDPOINT"
    add_secret "OTEL_EXPORTER" "$CFG_OTEL_EXPORTER"

    fly secrets set -a "$app_name" "${SECRETS[@]}"
    ok "Secrets set"

    # Update fly.toml
    if grep -q "<your-app>" "$PROJECT_DIR/fly.toml"; then
        sed -i '' "s/<your-app>/$app_name/" "$PROJECT_DIR/fly.toml"
        ok "Updated fly.toml with app name"
    fi

    # Deploy
    info "Deploying..."
    (cd "$PROJECT_DIR" && fly deploy)
    ok "Deployed!"

    # Wait for health
    echo ""
    info "Waiting for health check..."
    local attempts=0
    while [ $attempts -lt 30 ]; do
        if curl -sf "${dashboard_url}/health" >/dev/null 2>&1; then
            ok "Health check passed!"
            break
        fi
        sleep 2
        attempts=$((attempts + 1))
    done
    if [ $attempts -ge 30 ]; then
        warn "Health check timed out — the app may still be starting"
        info "Check manually: curl ${dashboard_url}/health"
    fi

    # Success
    echo ""
    echo "╔══════════════════════════════════════╗"
    echo "║          Setup Complete!             ║"
    echo "╚══════════════════════════════════════╝"
    echo ""
    info "Next steps:"
    echo "  1. Set your Slack Event Subscriptions URL to:"
    echo "     ${dashboard_url}/slack/events"
    echo ""
    echo "  2. Invite the bot to a channel:"
    echo "     /invite @${CFG_BOT_NAME:-Ponko}"
    echo ""
    echo "  3. Say hello:"
    echo "     @${CFG_BOT_NAME:-Ponko} hello"
    echo ""
    if [ -n "$CFG_DASHBOARD_CLIENT_ID" ]; then
        echo "  4. Dashboard: ${dashboard_url}"
        echo "     Set OAuth redirect URL to: ${dashboard_url}/api/auth/slack/callback"
        echo ""
    fi
}

# ── Docker Deploy ─────────────────────────────────────

deploy_docker() {
    info "Generating .env for Docker..."

    load_config

    ENV_FILE="$PROJECT_DIR/.env"
    cat > "$ENV_FILE" <<EOF
DATABASE_URL=postgres://agent:agent@db:5432/agent?sslmode=disable
ANTHROPIC_API_KEY=${CFG_ANTHROPIC_KEY}
SLACK_BOT_TOKEN=${CFG_BOT_TOKEN}
SLACK_SIGNING_SECRET=${CFG_SIGNING_SECRET}
SLACK_BOT_USER_ID=${CFG_BOT_USER_ID}
PONKO_API_KEY=${CFG_API_KEY}
COOKIE_SIGNING_KEY=${CFG_COOKIE_KEY}
BOT_NAME=${CFG_BOT_NAME:-Ponko}
TIMEZONE=${CFG_TIMEZONE:-America/Los_Angeles}
WORKER_CONCURRENCY=${CFG_WORKER_CONCURRENCY:-10}
EOF

    add_env "SYSTEM_PROMPT" "$CFG_SYSTEM_PROMPT"
    add_env "DASHBOARD_URL" "$CFG_URL"

    # Dashboard OAuth
    add_env "SLACK_CLIENT_ID" "$CFG_DASHBOARD_CLIENT_ID"
    add_env "SLACK_CLIENT_SECRET" "$CFG_DASHBOARD_CLIENT_SECRET"
    add_env "SLACK_TEAM_ID" "$CFG_DASHBOARD_TEAM_ID"

    # MCP
    [ "$CFG_MCP_URLS" != "[]" ] && add_env "MCP_SERVER_URLS" "$CFG_MCP_URLS"
    add_env "MCP_ACCESS_KEY" "$CFG_MCP_KEY"

    # GitHub MCP
    add_env "GITHUB_MCP_URL" "$CFG_GITHUB_URL"
    add_env "GITHUB_PAT" "$CFG_GITHUB_PAT"

    # Linear MCP
    add_env "LINEAR_MCP_URL" "$CFG_LINEAR_URL"
    add_env "LINEAR_MCP_ACCESS_TOKEN" "$CFG_LINEAR_TOKEN"
    add_env "LINEAR_MCP_TOKEN_URL" "$CFG_LINEAR_TOKEN_URL"
    add_env "LINEAR_MCP_CLIENT_ID" "$CFG_LINEAR_CLIENT_ID"
    add_env "LINEAR_MCP_CLIENT_SECRET" "$CFG_LINEAR_CLIENT_SECRET"
    add_env "LINEAR_MCP_REFRESH_TOKEN" "$CFG_LINEAR_REFRESH"

    # Observability
    add_env "OTEL_EXPORTER_OTLP_ENDPOINT" "$CFG_OTEL_ENDPOINT"
    add_env "OTEL_EXPORTER" "$CFG_OTEL_EXPORTER"

    ok "Generated .env"

    echo ""
    info "To start:"
    echo "  docker compose up -d"
    echo ""
    info "Bot will run at http://localhost:8080"
    info "Use ngrok or similar to expose for Slack events"
}

# ── Deploy Mode ───────────────────────────────────────

deploy_mode() {
    [ ! -f "$CONFIG_FILE" ] && die "ponko.yaml not found. Run ./scripts/setup.sh first"

    load_config

    case "$CFG_PLATFORM" in
        fly)
            check_fly_auth
            deploy_fly
            ;;
        docker)
            deploy_docker
            ;;
        *)
            die "Unknown platform: $CFG_PLATFORM. Set deploy_platform to 'fly' or 'docker' in ponko.yaml"
            ;;
    esac
}

# ── Validate Mode ─────────────────────────────────────

validate_mode() {
    echo ""
    info "Validating Ponko configuration..."
    echo ""

    local errors=0

    # Check config exists
    if [ ! -f "$CONFIG_FILE" ]; then
        err "ponko.yaml not found"
        die "Run ./scripts/setup.sh to create it"
    fi
    ok "ponko.yaml exists"

    # Check required values
    local required_keys="slack_bot_token slack_signing_secret slack_bot_user_id anthropic_api_key"
    for key in $required_keys; do
        local val
        val=$(yaml_get "$key")
        if [ -z "$val" ]; then
            err "Missing required value: $key"
            errors=$((errors + 1))
        else
            ok "$key is set"
        fi
    done

    # Check health endpoint
    local url
    url=$(yaml_get "deploy_url")
    if [ -n "$url" ]; then
        info "Checking health at $url..."
        if curl -sf "${url}/health" >/dev/null 2>&1; then
            ok "Health check passed"
        else
            err "Health check failed: ${url}/health"
            errors=$((errors + 1))
        fi
    else
        warn "No deploy URL set — skipping health check"
    fi

    echo ""
    if [ $errors -eq 0 ]; then
        ok "All checks passed!"
    else
        err "$errors check(s) failed"
        exit 1
    fi
}

# ── Main ──────────────────────────────────────────────

case "${1:-}" in
    deploy)
        deploy_mode
        ;;
    validate)
        validate_mode
        ;;
    *)
        interactive_setup
        ;;
esac
