#!/bin/bash

# ============================================
# Blue-Green Deployment Script
# Zero Downtime Deployment
# ============================================

set -e

# --- Configuration ---
DEPLOY_PATH="${DEPLOY_PATH:-/opt/goshort}"
REGISTRY="${CI_REGISTRY}"
IMAGE_BACKEND="${CI_REGISTRY_IMAGE}/backend:${TAG:-latest}"
IMAGE_FRONTEND="${CI_REGISTRY_IMAGE}/frontend:${TAG:-latest}"
HEALTH_CHECK_TIMEOUT=60
HEALTH_CHECK_INTERVAL=2

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# --- Helper Functions ---
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

check_health() {
    local container_name=$1
    local max_attempts=$((HEALTH_CHECK_TIMEOUT / HEALTH_CHECK_INTERVAL))
    
    log_info "Checking health of $container_name..."
    
    for i in $(seq 1 $max_attempts); do
        if docker exec "$container_name" curl -f http://localhost:8080/api/v1/health >/dev/null 2>&1; then
            log_success "$container_name is healthy!"
            return 0
        fi
        sleep $HEALTH_CHECK_INTERVAL
    done
    
    log_error "$container_name failed health check"
    return 1
}

switch_nginx() {
    local target_color=$1
    log_info "Switching nginx to $target_color environment..."
    
    # Update upstream config
    echo "server backend-${target_color}:8080 max_fails=3 fail_timeout=30s;" > "${DEPLOY_PATH}/nginx/upstreams/backend_active.conf"
    
    NGINX_CONTAINER=$(docker ps --format '{{.Names}}' | grep nginx | head -1 || true)
    
    # Start Nginx if not running
    if [ -z "$NGINX_CONTAINER" ]; then
        log_warning "Nginx container not found, starting it..."
        docker compose -f docker-compose.prod.yml up -d nginx
        sleep 5
        NGINX_CONTAINER=$(docker ps --format '{{.Names}}' | grep nginx | head -1)
    fi

    # Test and Reload
    if ! docker exec "$NGINX_CONTAINER" nginx -t 2>&1 | grep -q "successful"; then
        log_error "Nginx configuration test failed"
        return 1
    fi
    
    docker exec "$NGINX_CONTAINER" nginx -s reload
    log_success "Nginx switched to $target_color"
    return 0
}

# --- Main Logic ---
main() {
    log_info "Starting Deployment Process..."
    
    if [ ! -d "$DEPLOY_PATH" ]; then
        log_error "Deployment path $DEPLOY_PATH does not exist"
        exit 1
    fi

    cd "$DEPLOY_PATH" || exit 1
    
    # Login
    echo "${CI_REGISTRY_PASSWORD}" | docker login -u "${CI_REGISTRY_USER}" --password-stdin "${CI_REGISTRY}"
    
    # Pull images
    log_info "Pulling images..."
    docker pull "$IMAGE_BACKEND"
    docker pull "$IMAGE_FRONTEND"
    
    # Determine Colors
    if docker ps --format '{{.Names}}' | grep -q "goshort-backend-blue"; then
        ACTIVE_COLOR="blue"
        INACTIVE_COLOR="green"
    else
        ACTIVE_COLOR="green"
        INACTIVE_COLOR="blue"
    fi
    
    log_info "Active: $ACTIVE_COLOR | Deploying to: $INACTIVE_COLOR"
    
    # 1. Start New Backend
    log_info "Starting backend-$INACTIVE_COLOR..."
    docker compose -f docker-compose.prod.yml up -d "backend-${INACTIVE_COLOR}"
    
    # 2. Health Check
    if ! check_health "goshort-backend-${INACTIVE_COLOR}"; then
        log_error "Health check failed. Stopping new container."
        docker stop "goshort-backend-${INACTIVE_COLOR}"
        exit 1
    fi
    
    # 3. Switch Nginx
    if ! switch_nginx "$INACTIVE_COLOR"; then
        exit 1
    fi
    
    # 4. Stop Old Backend (Immediate cleanup to save RAM)
    if [ -n "$ACTIVE_COLOR" ]; then
        log_info "Stopping old backend ($ACTIVE_COLOR) to free up memory..."
        docker stop "goshort-backend-${ACTIVE_COLOR}" || true
        docker rm "goshort-backend-${ACTIVE_COLOR}" || true
    fi

    # 5. Update Frontend (Safe now that RAM is freed)
    log_info "Updating frontend..."
    docker compose -f docker-compose.prod.yml up -d frontend

    # 6. Cleanup
    docker image prune -af --filter "until=24h" >/dev/null 2>&1 || true
    
    log_success "Deployment Complete! Active: $INACTIVE_COLOR"
}

main "$@"