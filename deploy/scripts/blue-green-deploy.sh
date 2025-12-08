#!/bin/bash

# ============================================
# Blue-Green Deployment Script
# Zero Downtime Deployment with Crash Recovery
# ============================================

set -e

# --- Configuration ---
DEPLOY_PATH="${DEPLOY_PATH:-/opt/goshort}"
REGISTRY="${CI_REGISTRY}"
IMAGE_BACKEND="${CI_REGISTRY_IMAGE}/backend:${TAG:-latest}"
IMAGE_FRONTEND="${CI_REGISTRY_IMAGE}/frontend:${TAG:-latest}"
HEALTH_CHECK_TIMEOUT=60
HEALTH_CHECK_INTERVAL=2
# مسیرهای فایل‌های پیکربندی
PROD_COMPOSE_FILE="docker-compose.prod.yml"
MONITORING_COMPOSE_FILE="docker-compose.monitoring.yml"
COMPOSE_FILES="-f $PROD_COMPOSE_FILE -f $MONITORING_COMPOSE_FILE"

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
        
        if [ $((i % 3)) -eq 0 ]; then
             log_info "Attempt $i/$max_attempts: waiting for health..."
        fi
        sleep $HEALTH_CHECK_INTERVAL
    done
    
    log_error "$container_name failed health check!"
    log_warning "================ DEBUG INFO START ================"
    echo "1. Checking curl:"
    docker exec "$container_name" which curl || echo "CURL NOT FOUND"
    echo "2. Curl output:"
    docker exec "$container_name" curl -v http://localhost:8080/api/v1/health || true
    echo "3. Logs:"
    docker logs --tail 50 "$container_name"
    log_warning "================ DEBUG INFO END ================"

    return 1
}

switch_nginx() {
    local target_color=$1
    log_info "Switching nginx to $target_color environment..."
    
    # Ensure directory exists
    mkdir -p "${DEPLOY_PATH}/nginx/upstreams"
    
    # Write the NEW config file pointing to the healthy container
    echo "server backend-${target_color}:8080 max_fails=3 fail_timeout=30s;" > "${DEPLOY_PATH}/nginx/upstreams/backend_active.conf"
    
    # Find Nginx container
    NGINX_CONTAINER=$(docker ps -a --format '{{.Names}}' | grep goshort-nginx | head -1 || true) # از goshort-nginx استفاده شد
    
    # Check Nginx status
    if [ -z "$NGINX_CONTAINER" ]; then
        log_warning "Nginx container not found, starting it..."
        # استفاده از هر دو فایل در صورت نیاز به راه‌اندازی مجدد
        docker compose $COMPOSE_FILES up -d nginx
    else
        STATUS=$(docker inspect --format='{{.State.Status}}' "$NGINX_CONTAINER" 2>/dev/null || echo "unknown")
        
        # If Nginx is in a crash loop (restarting/exited), FORCE RECREATE it.
        if [ "$STATUS" == "restarting" ] || [ "$STATUS" == "exited" ] || [ "$STATUS" == "dead" ]; then
            log_warning "Nginx is in '$STATUS' state. Performing aggressive restart..."
            docker stop "$NGINX_CONTAINER" || true
            docker rm -f "$NGINX_CONTAINER" || true
            sleep 2
            # استفاده از هر دو فایل در صورت نیاز به راه‌اندازی مجدد
            docker compose $COMPOSE_FILES up -d nginx
        fi
    fi

    log_info "Waiting for Nginx to stabilize..."
    sleep 5
    
    NGINX_CONTAINER=$(docker ps --format '{{.Names}}' | grep goshort-nginx | head -1)

    log_info "Testing Nginx configuration..."
    if ! docker exec "$NGINX_CONTAINER" nginx -t; then
        log_error "Nginx configuration test failed!"
        log_warning "Nginx Logs:"
        docker logs --tail 20 "$NGINX_CONTAINER"
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
    
    echo "${CI_REGISTRY_PASSWORD}" | docker login -u "${CI_REGISTRY_USER}" --password-stdin "${CI_REGISTRY}"
    
    log_info "Pulling images..."
    docker pull "$IMAGE_BACKEND"
    docker pull "$IMAGE_FRONTEND"
    
    if docker ps --format '{{.Names}}' | grep -q "goshort-backend-blue"; then
        ACTIVE_COLOR="blue"
        INACTIVE_COLOR="green"
    else
        ACTIVE_COLOR="green"
        INACTIVE_COLOR="blue"
    fi
    
    log_info "Active: $ACTIVE_COLOR | Deploying to: $INACTIVE_COLOR"
    
    # 0. Update all Monitoring and Infrastructure services (Loki, Promtail, Postgres, Redis, etc.)
    # این دستور تمام سرویس‌های غیر-Blue/Green را آپدیت می‌کند (شامل Loki/Promtail/Prometheus/Exporters)
    log_info "Updating infrastructure and monitoring services..."
    docker compose $COMPOSE_FILES up -d postgres redis prometheus grafana loki promtail postgres_exporter redis_exporter

    # 1. Start the NEW Backend
    log_info "Starting backend-$INACTIVE_COLOR..."
    # راه‌اندازی بک‌اند جدید با استفاده از هر دو فایل
    docker compose $COMPOSE_FILES up -d "backend-${INACTIVE_COLOR}"
    
    # 2. Check Health of NEW Backend
    if ! check_health "goshort-backend-${INACTIVE_COLOR}"; then
        log_error "Health check failed. Stopping new container."
        docker stop "goshort-backend-${INACTIVE_COLOR}"
        exit 1
    fi
    
    # 3. Update Frontend (MOVED UP: Before Nginx Switch)
    log_info "Updating frontend..."
    docker compose $COMPOSE_FILES up -d frontend

    # 4. Switch Nginx
    if ! switch_nginx "$INACTIVE_COLOR"; then
        exit 1
    fi
    
    # 5. Cleanup Old Backend
    if [ -n "$ACTIVE_COLOR" ]; then
        log_info "Stopping old backend ($ACTIVE_COLOR) to free up memory..."
        docker stop "goshort-backend-${ACTIVE_COLOR}" || true
        docker rm "goshort-backend-${ACTIVE_COLOR}" || true
    fi

    docker image prune -af --filter "until=24h" >/dev/null 2>&1 || true
    
    log_success "Deployment Complete! Active: $INACTIVE_COLOR"
}

main "$@"