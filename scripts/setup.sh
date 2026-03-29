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

# Read a value from ponko.yaml (flat YAML only — key: value or key: "value")
yaml_get() {
    local key="$1"
    grep -E "^[[:space:]]*${key}:" "$CONFIG_FILE" 2>/dev/null \
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
    if grep -qE "^[[:space:]]*${key}:" "$CONFIG_FILE" 2>/dev/null; then
        sed -i '' "s|^\([[:space:]]*${key}:\).*|\1 \"${value}\"|" "$CONFIG_FILE"
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

    yaml_set "platform" "$platform"

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
    yaml_set "bot_token" "$bot_token"

    local signing_secret
    signing_secret=$(prompt_value "Slack Signing Secret")
    [ -z "$signing_secret" ] && die "Signing secret is required"
    yaml_set "signing_secret" "$signing_secret"

    local bot_user_id
    bot_user_id=$(prompt_value "Slack Bot User ID (U...)")
    [ -z "$bot_user_id" ] && die "Bot user ID is required"
    yaml_set "bot_user_id" "$bot_user_id"

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
    yaml_set "name" "$bot_name"

    local timezone
    timezone=$(prompt_value "Timezone" "America/Los_Angeles")
    yaml_set "timezone" "$timezone"

    local system_prompt
    system_prompt=$(prompt_value "System prompt (blank for default)" "")
    if [ -n "$system_prompt" ]; then
        yaml_set "system_prompt" "$system_prompt"
    fi

    # Auto-generate secrets
    local api_key
    api_key=$(openssl rand -hex 32)
    yaml_set "api_key" "$api_key"
    ok "Generated API key"

    local cookie_key
    cookie_key=$(openssl rand -hex 32)
    yaml_set "cookie_signing_key" "$cookie_key"
    ok "Generated cookie signing key"

    # Optional: Dashboard OAuth
    echo ""
    if prompt_yn "Set up dashboard OAuth? (allows Slack login to admin UI)"; then
        local client_id
        client_id=$(prompt_value "Slack Client ID")
        yaml_set "slack_client_id" "$client_id"

        local client_secret
        client_secret=$(prompt_value "Slack Client Secret")
        yaml_set "slack_client_secret" "$client_secret"

        local team_id
        team_id=$(prompt_value "Slack Team ID (T...)")
        yaml_set "slack_team_id" "$team_id"
    fi

    # Optional: MCP
    echo ""
    if prompt_yn "Configure MCP tool servers?"; then
        local mcp_urls
        mcp_urls=$(prompt_value "MCP Server URLs (comma-separated)")
        yaml_set "server_urls" "$mcp_urls"

        local mcp_key
        mcp_key=$(prompt_value "MCP Access Key (blank if none)" "")
        if [ -n "$mcp_key" ]; then
            yaml_set "access_key" "$mcp_key"
        fi
    fi

    # Optional: GitHub MCP
    echo ""
    if prompt_yn "Configure GitHub MCP integration?"; then
        local github_url
        github_url=$(prompt_value "GitHub MCP server URL")
        yaml_set "mcp_url" "$github_url"

        local github_pat
        github_pat=$(prompt_value "GitHub Personal Access Token")
        yaml_set "pat" "$github_pat"
    fi

    # Optional: Linear MCP
    echo ""
    if prompt_yn "Configure Linear MCP integration?"; then
        local linear_url
        linear_url=$(prompt_value "Linear MCP server URL")
        # Use a unique grep match — linear section has its own mcp_url
        sed -i '' "/^# ── Linear/,/^# ──/{s|^\([[:space:]]*mcp_url:\).*|\1 \"${linear_url}\"|;}" "$CONFIG_FILE"

        local linear_token
        linear_token=$(prompt_value "Linear access token")
        yaml_set "access_token" "$linear_token"

        local linear_token_url
        linear_token_url=$(prompt_value "Linear OAuth token URL")
        yaml_set "token_url" "$linear_token_url"

        local linear_client_id
        linear_client_id=$(prompt_value "Linear OAuth client ID")
        yaml_set "client_id" "$linear_client_id"

        local linear_client_secret
        linear_client_secret=$(prompt_value "Linear OAuth client secret")
        yaml_set "client_secret" "$linear_client_secret"

        local linear_refresh
        linear_refresh=$(prompt_value "Linear OAuth refresh token")
        yaml_set "refresh_token" "$linear_refresh"
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

    local app_name
    app_name=$(yaml_get "app_name")
    if [ -z "$app_name" ]; then
        local random_suffix
        random_suffix=$(openssl rand -hex 2)
        app_name=$(prompt_value "App name" "ponko-${random_suffix}")
        yaml_set "app_name" "$app_name"
    fi

    local region
    region=$(yaml_get "region")
    region="${region:-iad}"

    local dashboard_url
    dashboard_url=$(yaml_get "url")
    if [ -z "$dashboard_url" ]; then
        dashboard_url="https://${app_name}.fly.dev"
        yaml_set "url" "$dashboard_url"
    fi

    # Create app
    info "Creating Fly.io app: $app_name"
    if fly apps list 2>/dev/null | grep -q "$app_name"; then
        warn "App $app_name already exists, skipping creation"
    else
        fly apps create "$app_name" --machines
        ok "App created"
    fi

    # Create and attach Postgres
    local db_name="${app_name}-db"
    info "Creating Postgres: $db_name"
    if fly apps list 2>/dev/null | grep -q "$db_name"; then
        warn "Database $db_name already exists, skipping creation"
    else
        fly postgres create --name "$db_name" --region "$region" --initial-cluster-size 1 --vm-size shared-cpu-1x --volume-size 1
        ok "Postgres created"
    fi

    info "Attaching database..."
    fly postgres attach "$db_name" --app "$app_name" 2>/dev/null || warn "Database may already be attached"

    # Set secrets
    info "Setting secrets..."
    local bot_token signing_secret bot_user_id anthropic_key api_key cookie_key
    local bot_name timezone worker_concurrency system_prompt
    bot_token=$(yaml_get "bot_token")
    signing_secret=$(yaml_get "signing_secret")
    bot_user_id=$(yaml_get "bot_user_id")
    anthropic_key=$(yaml_get "anthropic_api_key")
    api_key=$(yaml_get "api_key")
    cookie_key=$(yaml_get "cookie_signing_key")
    bot_name=$(yaml_get "name")
    timezone=$(yaml_get "timezone")
    worker_concurrency=$(yaml_get "worker_concurrency")
    system_prompt=$(yaml_get "system_prompt")

    local secrets_cmd="fly secrets set -a $app_name"
    secrets_cmd+=" ANTHROPIC_API_KEY=$anthropic_key"
    secrets_cmd+=" SLACK_BOT_TOKEN=$bot_token"
    secrets_cmd+=" SLACK_SIGNING_SECRET=$signing_secret"
    secrets_cmd+=" SLACK_BOT_USER_ID=$bot_user_id"
    secrets_cmd+=" PONKO_API_KEY=$api_key"
    secrets_cmd+=" COOKIE_SIGNING_KEY=$cookie_key"
    secrets_cmd+=" DASHBOARD_URL=$dashboard_url"
    [ -n "$bot_name" ] && [ "$bot_name" != "Ponko" ] && secrets_cmd+=" BOT_NAME=$bot_name"
    [ -n "$timezone" ] && [ "$timezone" != "America/Los_Angeles" ] && secrets_cmd+=" TIMEZONE=$timezone"
    [ -n "$worker_concurrency" ] && secrets_cmd+=" WORKER_CONCURRENCY=$worker_concurrency"
    [ -n "$system_prompt" ] && secrets_cmd+=" SYSTEM_PROMPT=$system_prompt"

    # Optional: Dashboard OAuth
    local client_id client_secret team_id
    client_id=$(yaml_get "slack_client_id")
    client_secret=$(yaml_get "slack_client_secret")
    team_id=$(yaml_get "slack_team_id")
    [ -n "$client_id" ] && secrets_cmd+=" SLACK_CLIENT_ID=$client_id"
    [ -n "$client_secret" ] && secrets_cmd+=" SLACK_CLIENT_SECRET=$client_secret"
    [ -n "$team_id" ] && secrets_cmd+=" SLACK_TEAM_ID=$team_id"

    # Optional: MCP
    local mcp_urls mcp_key
    mcp_urls=$(yaml_get "server_urls")
    mcp_key=$(yaml_get "access_key")
    [ -n "$mcp_urls" ] && [ "$mcp_urls" != "[]" ] && secrets_cmd+=" MCP_SERVER_URLS=$mcp_urls"
    [ -n "$mcp_key" ] && secrets_cmd+=" MCP_ACCESS_KEY=$mcp_key"

    # Optional: GitHub MCP
    local github_url github_pat
    github_url=$(yaml_get "mcp_url")
    github_pat=$(yaml_get "pat")
    [ -n "$github_url" ] && secrets_cmd+=" GITHUB_MCP_URL=$github_url"
    [ -n "$github_pat" ] && secrets_cmd+=" GITHUB_PAT=$github_pat"

    # Optional: Linear MCP
    local linear_url linear_token linear_token_url linear_client_id linear_client_secret linear_refresh
    linear_url=$(yaml_get "mcp_url")
    linear_token=$(yaml_get "access_token")
    linear_token_url=$(yaml_get "token_url")
    linear_client_id=$(yaml_get "client_id")
    linear_client_secret=$(yaml_get "client_secret")
    linear_refresh=$(yaml_get "refresh_token")
    [ -n "$linear_url" ] && secrets_cmd+=" LINEAR_MCP_URL=$linear_url"
    [ -n "$linear_token" ] && secrets_cmd+=" LINEAR_MCP_ACCESS_TOKEN=$linear_token"
    [ -n "$linear_token_url" ] && secrets_cmd+=" LINEAR_MCP_TOKEN_URL=$linear_token_url"
    [ -n "$linear_client_id" ] && secrets_cmd+=" LINEAR_MCP_CLIENT_ID=$linear_client_id"
    [ -n "$linear_client_secret" ] && secrets_cmd+=" LINEAR_MCP_CLIENT_SECRET=$linear_client_secret"
    [ -n "$linear_refresh" ] && secrets_cmd+=" LINEAR_MCP_REFRESH_TOKEN=$linear_refresh"

    # Optional: Observability
    local otel_endpoint otel_exporter
    otel_endpoint=$(yaml_get "otel_endpoint")
    otel_exporter=$(yaml_get "otel_exporter")
    [ -n "$otel_endpoint" ] && secrets_cmd+=" OTEL_EXPORTER_OTLP_ENDPOINT=$otel_endpoint"
    [ -n "$otel_exporter" ] && secrets_cmd+=" OTEL_EXPORTER=$otel_exporter"

    eval "$secrets_cmd"
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
    echo "     /invite @${bot_name:-Ponko}"
    echo ""
    echo "  3. Say hello:"
    echo "     @${bot_name:-Ponko} hello"
    echo ""
    if [ -n "$client_id" ]; then
        echo "  4. Dashboard: ${dashboard_url}"
        echo "     Set OAuth redirect URL to: ${dashboard_url}/api/auth/slack/callback"
        echo ""
    fi
}

# ── Docker Deploy ─────────────────────────────────────

deploy_docker() {
    info "Generating .env for Docker..."

    local env_file="$PROJECT_DIR/.env"
    local bot_token signing_secret bot_user_id anthropic_key api_key cookie_key
    local bot_name timezone worker_concurrency dashboard_url system_prompt
    bot_token=$(yaml_get "bot_token")
    signing_secret=$(yaml_get "signing_secret")
    bot_user_id=$(yaml_get "bot_user_id")
    anthropic_key=$(yaml_get "anthropic_api_key")
    api_key=$(yaml_get "api_key")
    cookie_key=$(yaml_get "cookie_signing_key")
    bot_name=$(yaml_get "name")
    timezone=$(yaml_get "timezone")
    worker_concurrency=$(yaml_get "worker_concurrency")
    dashboard_url=$(yaml_get "url")
    system_prompt=$(yaml_get "system_prompt")

    cat > "$env_file" <<EOF
DATABASE_URL=postgres://agent:agent@db:5432/agent?sslmode=disable
ANTHROPIC_API_KEY=${anthropic_key}
SLACK_BOT_TOKEN=${bot_token}
SLACK_SIGNING_SECRET=${signing_secret}
SLACK_BOT_USER_ID=${bot_user_id}
PONKO_API_KEY=${api_key}
COOKIE_SIGNING_KEY=${cookie_key}
BOT_NAME=${bot_name:-Ponko}
TIMEZONE=${timezone:-America/Los_Angeles}
WORKER_CONCURRENCY=${worker_concurrency:-10}
EOF

    [ -n "$system_prompt" ] && echo "SYSTEM_PROMPT=${system_prompt}" >> "$env_file"
    [ -n "$dashboard_url" ] && echo "DASHBOARD_URL=${dashboard_url}" >> "$env_file"

    # Dashboard OAuth
    local client_id client_secret team_id
    client_id=$(yaml_get "slack_client_id")
    client_secret=$(yaml_get "slack_client_secret")
    team_id=$(yaml_get "slack_team_id")
    [ -n "$client_id" ] && echo "SLACK_CLIENT_ID=${client_id}" >> "$env_file"
    [ -n "$client_secret" ] && echo "SLACK_CLIENT_SECRET=${client_secret}" >> "$env_file"
    [ -n "$team_id" ] && echo "SLACK_TEAM_ID=${team_id}" >> "$env_file"

    # MCP
    local mcp_urls mcp_key
    mcp_urls=$(yaml_get "server_urls")
    mcp_key=$(yaml_get "access_key")
    [ -n "$mcp_urls" ] && [ "$mcp_urls" != "[]" ] && echo "MCP_SERVER_URLS=${mcp_urls}" >> "$env_file"
    [ -n "$mcp_key" ] && echo "MCP_ACCESS_KEY=${mcp_key}" >> "$env_file"

    # GitHub MCP
    local github_url github_pat
    github_url=$(yaml_get "mcp_url")
    github_pat=$(yaml_get "pat")
    [ -n "$github_url" ] && echo "GITHUB_MCP_URL=${github_url}" >> "$env_file"
    [ -n "$github_pat" ] && echo "GITHUB_PAT=${github_pat}" >> "$env_file"

    # Linear MCP
    local linear_token linear_token_url linear_client_id linear_client_secret linear_refresh
    linear_token=$(yaml_get "access_token")
    linear_token_url=$(yaml_get "token_url")
    linear_client_id=$(yaml_get "client_id")
    linear_client_secret=$(yaml_get "client_secret")
    linear_refresh=$(yaml_get "refresh_token")
    # linear mcp_url conflicts with github mcp_url in yaml_get — read from Linear section
    local linear_url
    linear_url=$(sed -n '/^# ── Linear/,/^# ──/{/mcp_url:/p;}' "$CONFIG_FILE" 2>/dev/null | sed 's/^[^:]*:[[:space:]]*//' | sed 's/^"//' | sed 's/"$//')
    [ -n "$linear_url" ] && echo "LINEAR_MCP_URL=${linear_url}" >> "$env_file"
    [ -n "$linear_token" ] && echo "LINEAR_MCP_ACCESS_TOKEN=${linear_token}" >> "$env_file"
    [ -n "$linear_token_url" ] && echo "LINEAR_MCP_TOKEN_URL=${linear_token_url}" >> "$env_file"
    [ -n "$linear_client_id" ] && echo "LINEAR_MCP_CLIENT_ID=${linear_client_id}" >> "$env_file"
    [ -n "$linear_client_secret" ] && echo "LINEAR_MCP_CLIENT_SECRET=${linear_client_secret}" >> "$env_file"
    [ -n "$linear_refresh" ] && echo "LINEAR_MCP_REFRESH_TOKEN=${linear_refresh}" >> "$env_file"

    # Observability
    local otel_endpoint otel_exporter
    otel_endpoint=$(yaml_get "otel_endpoint")
    otel_exporter=$(yaml_get "otel_exporter")
    [ -n "$otel_endpoint" ] && echo "OTEL_EXPORTER_OTLP_ENDPOINT=${otel_endpoint}" >> "$env_file"
    [ -n "$otel_exporter" ] && echo "OTEL_EXPORTER=${otel_exporter}" >> "$env_file"

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

    local platform
    platform=$(yaml_get "platform")

    case "$platform" in
        fly)
            check_fly_auth
            deploy_fly
            ;;
        docker)
            deploy_docker
            ;;
        *)
            die "Unknown platform: $platform. Set deploy.platform to 'fly' or 'docker' in ponko.yaml"
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
    local required_keys="bot_token signing_secret bot_user_id anthropic_api_key"
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
    url=$(yaml_get "url")
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
