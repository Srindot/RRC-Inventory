#!/bin/bash

# RRC Inventory Management Script

set -e

show_help() {
    echo "RRC Inventory Management Script"
    echo ""
    echo "Usage: $0 [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  dev-start       Start development environment"
    echo "  dev-stop        Stop development environment"
    echo "  dev-logs        Show development logs"
    echo "  prod-start      Start production environment"
    echo "  prod-stop       Stop production environment"
    echo "  prod-logs       Show production logs"
    echo "  build           Build all services for production"
    echo "  clean           Clean up containers and volumes"
    echo "  reset-db        Reset database (WARNING: destroys all data)"
    echo "  help            Show this help message"
}

dev_start() {
    echo "🚀 Starting development environment..."
    docker-compose -f docker-compose.dev.yml up -d
    echo "✅ Development environment started!"
    echo "📱 Frontend: http://localhost"
    echo "🔧 Backend API: http://localhost/api"
    echo "🗄️ Database: localhost:5432"
}

dev_stop() {
    echo "🛑 Stopping development environment..."
    docker-compose -f docker-compose.dev.yml down
    echo "✅ Development environment stopped!"
}

dev_logs() {
    if [ -n "$2" ]; then
        docker-compose -f docker-compose.dev.yml logs -f "$2"
    else
        docker-compose -f docker-compose.dev.yml logs -f
    fi
}

prod_start() {
    echo "🚀 Starting production environment..."
    docker-compose up -d
    echo "✅ Production environment started!"
    echo "🌐 Application: http://localhost"
}

prod_stop() {
    echo "🛑 Stopping production environment..."
    docker-compose down
    echo "✅ Production environment stopped!"
}

prod_logs() {
    if [ -n "$2" ]; then
        docker-compose logs -f "$2"
    else
        docker-compose logs -f
    fi
}

build_all() {
    echo "🔨 Building all services for production..."
    docker-compose build --no-cache
    echo "✅ All services built!"
}

clean_all() {
    echo "🧹 Cleaning up containers and images..."
    docker-compose -f docker-compose.dev.yml down -v --remove-orphans
    docker-compose down -v --remove-orphans
    docker system prune -f
    echo "✅ Cleanup complete!"
}

reset_db() {
    echo "⚠️  WARNING: This will destroy ALL database data!"
    read -p "Are you sure? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🗑️ Resetting database..."
        docker-compose down
        docker volume rm rrc-inventory_postgres_data 2>/dev/null || true
        echo "✅ Database reset complete!"
    else
        echo "❌ Database reset cancelled."
    fi
}

case "$1" in
    dev-start)
        dev_start
        ;;
    dev-stop)
        dev_stop
        ;;
    dev-logs)
        dev_logs "$@"
        ;;
    prod-start)
        prod_start
        ;;
    prod-stop)
        prod_stop
        ;;
    prod-logs)
        prod_logs "$@"
        ;;
    build)
        build_all
        ;;
    clean)
        clean_all
        ;;
    reset-db)
        reset_db
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo "❌ Unknown command: $1"
        echo ""
        show_help
        exit 1
        ;;
esac