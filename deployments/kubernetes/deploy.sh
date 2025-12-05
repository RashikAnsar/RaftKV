#!/bin/bash
# RaftKV Kubernetes Deployment Helper Script

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

NAMESPACE="raftkv"
HELM_CHART="../../deployments/helm/raftkv"
VALUES_DEV="../../deployments/helm/raftkv/values-development.yaml"
VALUES_PROD="../../deployments/helm/raftkv/values-production.yaml"

# Helper functions
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}➜ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

check_requirements() {
    print_info "Checking requirements..."

    if ! command -v kubectl &> /dev/null; then
        print_error "kubectl not found. Please install kubectl."
        exit 1
    fi

    if ! command -v helm &> /dev/null; then
        print_error "helm not found. Please install helm."
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        print_error "Cannot connect to Kubernetes cluster."
        exit 1
    fi

    print_success "All requirements met"
}

build_image() {
    print_info "Building Docker image..."
    cd ../..
    make docker-build
    cd deployments/kubernetes
    print_success "Docker image built"
}

load_image_minikube() {
    print_info "Loading image to Minikube..."
    minikube image load raftkv:latest
    print_success "Image loaded to Minikube"
}

install() {
    local mode=$1
    check_requirements

    if [ "$mode" == "prod" ]; then
        print_info "Installing RaftKV (Production mode)..."
        helm install raftkv $HELM_CHART -n $NAMESPACE --create-namespace -f $VALUES_PROD
    else
        print_info "Installing RaftKV (Development mode)..."
        helm install raftkv $HELM_CHART -n $NAMESPACE --create-namespace -f $VALUES_DEV
    fi

    print_info "Waiting for pods to be ready..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=raftkv -n $NAMESPACE --timeout=120s || true

    print_success "Installation complete!"
    show_status
}

uninstall() {
    print_info "Uninstalling RaftKV..."
    helm uninstall raftkv -n $NAMESPACE || print_info "Release not found"
    print_success "Uninstall complete"
}

upgrade() {
    check_requirements
    print_info "Upgrading RaftKV..."
    helm upgrade raftkv $HELM_CHART -n $NAMESPACE -f $VALUES_DEV
    print_info "Waiting for rollout..."
    kubectl rollout status statefulset/raftkv -n $NAMESPACE --timeout=120s || true
    print_success "Upgrade complete!"
    show_status
}

restart() {
    print_info "Restarting RaftKV pods..."
    kubectl rollout restart statefulset/raftkv -n $NAMESPACE
    print_info "Waiting for rollout..."
    kubectl rollout status statefulset/raftkv -n $NAMESPACE --timeout=120s || true
    print_success "Restart complete!"
    show_status
}

show_status() {
    echo ""
    echo "================================"
    echo "RaftKV Cluster Status"
    echo "================================"
    echo ""
    echo "Pods:"
    kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv || echo "No pods found"
    echo ""
    echo "Services:"
    kubectl get svc -n $NAMESPACE || echo "No services found"
    echo ""
    echo "PVCs:"
    kubectl get pvc -n $NAMESPACE || echo "No PVCs found"
}

show_logs() {
    local pod=${1:-raftkv-0}
    kubectl logs -n $NAMESPACE $pod --tail=100 -f
}

port_forward() {
    print_info "Port-forwarding raftkv service to localhost:8080..."
    print_info "Access RaftKV at: http://localhost:8080"
    print_info "Press Ctrl+C to stop"
    kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080
}

clean() {
    print_info "Cleaning up RaftKV..."
    uninstall
    print_info "Deleting PVCs..."
    kubectl delete pvc -n $NAMESPACE --all || print_info "No PVCs to delete"
    print_info "Deleting namespace..."
    kubectl delete namespace $NAMESPACE || print_info "Namespace not found"
    print_success "Cleanup complete"
}

deploy() {
    check_requirements
    build_image
    load_image_minikube
    install "$1"
    echo ""
    echo "================================"
    echo "RaftKV deployed successfully!"
    echo "================================"
    echo ""
    echo "To access RaftKV, run:"
    echo "  $0 port-forward"
    echo ""
    echo "To view logs, run:"
    echo "  $0 logs"
}

show_help() {
    echo "RaftKV Kubernetes Deployment Helper"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  deploy         Build image, load to Minikube, and install (dev mode)"
    echo "  deploy-prod    Build image, load to Minikube, and install (prod mode)"
    echo "  install        Install RaftKV using Helm (dev mode)"
    echo "  install-prod   Install RaftKV using Helm (prod mode)"
    echo "  uninstall      Uninstall RaftKV"
    echo "  upgrade        Upgrade RaftKV after code changes"
    echo "  restart        Restart all RaftKV pods"
    echo "  status         Show cluster status"
    echo "  logs [pod]     Show logs (default: raftkv-0)"
    echo "  port-forward   Port-forward to localhost:8080"
    echo "  clean          Uninstall + delete PVCs + delete namespace"
    echo "  help           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 deploy              # Full deployment to Minikube"
    echo "  $0 status              # Check cluster status"
    echo "  $0 logs raftkv-1       # View logs from raftkv-1"
    echo "  $0 upgrade             # Upgrade after code changes"
}

# Main
case "$1" in
    deploy)
        deploy "dev"
        ;;
    deploy-prod)
        deploy "prod"
        ;;
    install)
        install "dev"
        ;;
    install-prod)
        install "prod"
        ;;
    uninstall)
        uninstall
        ;;
    upgrade)
        upgrade
        ;;
    restart)
        restart
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs "$2"
        ;;
    port-forward|pf)
        port_forward
        ;;
    clean)
        clean
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        print_error "Unknown command: $1"
        echo ""
        show_help
        exit 1
        ;;
esac
