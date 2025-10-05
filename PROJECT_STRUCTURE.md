# 📁 Project Structure

```
rrc-inventory/
├── 📄 README.md                     # Project documentation
├── 📄 docker-compose.yml            # Main Docker Compose configuration
├── 📄 manage.sh                     # Project management script
├── 📄 Caddyfile                     # Production Caddy configuration
├── 📄 Caddyfile.dev                 # Development Caddy configuration
├── 📄 .gitignore                    # Git ignore rules
├── 🖼️ rrc_logo.png                  # Project logo
│
├── 🔧 backend/                      # Go backend
│   ├── 📄 main.go                   # Main Go application
│   ├── 📄 go.mod                    # Go modules
│   ├── 📄 go.sum                    # Go dependencies
│   ├── 📄 Dockerfile                # Backend Docker configuration
│   ├── 📄 .air.toml                 # Air hot reload configuration
│   ├── 📄 .dockerignore             # Docker ignore rules
│   └── 📁 uploads/                  # Upload directory
│       └── 📄 .gitkeep              # Keep directory in git
│
└── 🎨 frontend/                     # SvelteKit frontend
    ├── 📄 package.json              # Node.js dependencies
    ├── 📄 package-lock.json         # Locked dependencies
    ├── 📄 svelte.config.js          # Svelte configuration
    ├── 📄 tsconfig.json             # TypeScript configuration
    ├── 📄 vite.config.ts            # Vite bundler configuration
    ├── 📄 Dockerfile                # Frontend Docker configuration
    ├── 📄 nginx.conf                # Nginx configuration for production
    ├── 📁 src/                      # Source code
    │   ├── 📄 app.html              # Main HTML template
    │   ├── 📄 app.d.ts              # TypeScript definitions
    │   ├── 📁 lib/                  # Shared libraries
    │   │   ├── 📄 config.js         # App configuration
    │   │   ├── 📄 index.ts          # Library exports
    │   │   └── 📁 assets/           # Static assets
    │   │       └── 🖼️ favicon.svg   # Site favicon
    │   └── 📁 routes/               # SvelteKit routes
    │       ├── 📄 +layout.svelte    # Global layout
    │       ├── 📄 +page.js          # Home page data
    │       ├── 📄 +page.svelte      # Home page component
    │       └── 📁 admin/            # Admin routes
    │           ├── 📄 +page.js      # Admin page data
    │           └── 📄 +page.svelte  # Admin page component
    ├── 📁 static/                   # Static files
    │   ├── 📄 robots.txt            # SEO robots file
    │   └── 🖼️ rrc_logo.png          # Logo image
    └── 📁 build/                    # Production build output
        └── ... (generated files)
```

## 🚀 Quick Commands

- **Start**: `./manage.sh dev-start`
- **Stop**: `./manage.sh dev-stop`  
- **Logs**: `./manage.sh dev-logs`
- **Clean**: `./manage.sh clean`

## 🌐 Access Points

- **Website**: http://localhost
- **Admin Panel**: http://localhost/admin
- **API**: http://localhost/api/*
- **Database**: localhost:5432