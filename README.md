# 🤖 RRC Inventory Management System

A modern web application for managing Robotics Research Centre lab equipment inventory built with Go, SvelteKit, PostgreSQL, and Caddy.

## 🏗️ Architecture

- **Backend**: Go with Gin framework
- **Frontend**: SvelteKit with development server
- **Database**: PostgreSQL 16
- **Reverse Proxy**: Caddy v2 (automatic HTTPS)
- **Orchestration**: Docker Compose

## 🚀 Quick Start

### Prerequisites
- Docker and Docker Compose
- Git

### Development Environment

1. **Clone the repository:**
   ```bash
   git clone <repository-url>
   cd rrc-inventory
   ```

2. **Start the development environment:**
   ```bash
   ./manage.sh dev-start
   ```

3. **Access the application:**
   - 🌐 **Frontend**: http://localhost
   - 🔧 **Backend API**: http://localhost/api
   - 🗄️ **Database**: localhost:5432
   - 👨‍💼 **Admin Panel**: http://localhost/admin

### Default Admin Login
- **Username**: `Srinath`
- **Password**: `rrc@srinath`

3. **View logs:**
   ```bash
   ./manage.sh dev-logs [service-name]
   ```

4. **Stop the development environment:**
   ```bash
   ./manage.sh dev-stop
   ```

### Production Environment

1. **Build all services:**
   ```bash
   ./manage.sh build
   ```

2. **Start production environment:**
   ```bash
   ./manage.sh prod-start
   ```

3. **Stop production environment:**
   ```bash
   ./manage.sh prod-stop
   ```

## 📁 Project Structure

```
rrc-inventory/
├── backend/                 # Go backend API
│   ├── main.go             # Main application
│   ├── go.mod              # Go dependencies
│   ├── Dockerfile          # Multi-stage Docker build
│   └── .air.toml           # Hot reload config
├── frontend/               # SvelteKit frontend
│   ├── src/                # Source code
│   │   ├── routes/         # Page routes
│   │   └── lib/            # Shared components
│   ├── package.json        # Node dependencies
│   ├── svelte.config.js    # SvelteKit config
│   ├── Dockerfile          # Multi-stage Docker build
│   └── nginx.conf          # Production web server config
├── docker-compose.yml      # Production orchestration
├── docker-compose.dev.yml  # Development orchestration
├── Caddyfile              # Production proxy config
├── Caddyfile.dev          # Development proxy config
├── manage.sh              # Management script
└── README.md              # This file
```

## 🛠️ Development

### Backend Development
- Hot reload with Air
- PostgreSQL database
- CORS enabled for development

### Frontend Development
- Vite dev server with hot reload
- Static build for production
- TypeScript support

### Database
- PostgreSQL 16 with Alpine
- Automatic migrations
- Persistent data volumes

## 📚 API Endpoints

### Items
- `GET /api/items` - List all items
- `POST /api/items` - Create new item

### Loans
- `GET /api/loans/active` - List active loans
- `POST /api/borrow` - Borrow an item
- `POST /api/return/:id` - Return an item

## 🐳 Docker Services

### Development
- **backend**: Go with hot reload
- **frontend**: Vite dev server
- **db**: PostgreSQL (exposed on port 5432)
- **caddy**: Reverse proxy

### Production
- **backend**: Compiled Go binary
- **frontend**: Static files with Nginx
- **db**: PostgreSQL (internal only)
- **caddy**: Reverse proxy with automatic HTTPS

## 🔧 Management Commands

Use the `./manage.sh` script for common tasks:

```bash
./manage.sh dev-start      # Start development
./manage.sh dev-stop       # Stop development
./manage.sh dev-logs       # View development logs
./manage.sh prod-start     # Start production
./manage.sh prod-stop      # Stop production
./manage.sh prod-logs      # View production logs
./manage.sh build          # Build all services
./manage.sh clean          # Clean up containers
./manage.sh reset-db       # Reset database (destroys data!)
./manage.sh help           # Show help
```

## 🔒 Security Features

- Automatic HTTPS with Caddy (production)
- Security headers
- CORS configuration
- Input validation
- SQL injection protection (GORM)

## 🚀 Deployment

### Local Development
```bash
./manage.sh dev-start
```

### Production (Local)
```bash
./manage.sh build
./manage.sh prod-start
```

### Production (Server)
1. Update `Caddyfile` with your domain
2. Set up DNS to point to your server
3. Run production commands
4. Caddy will automatically provision SSL certificates

## 📝 Environment Variables

### Backend
- `DATABASE_URL`: PostgreSQL connection string

### Database
- `POSTGRES_USER`: Database user
- `POSTGRES_PASSWORD`: Database password
- `POSTGRES_DB`: Database name

## 🐛 Troubleshooting

### Common Issues

1. **Port already in use**
   ```bash
   ./manage.sh dev-stop
   ./manage.sh clean
   ```

2. **Database connection issues**
   ```bash
   ./manage.sh dev-logs db
   ```

3. **Frontend build issues**
   ```bash
   docker-compose build --no-cache frontend
   ```

4. **View service logs**
   ```bash
   ./manage.sh dev-logs [backend|frontend|db|caddy]
   ```

### Reset Everything
```bash
./manage.sh clean
./manage.sh reset-db
./manage.sh dev-start
```

## 📄 License

This project is licensed under the MIT License.