# Development stage
FROM node:20-alpine AS development
WORKDIR /app

# Copy package files
COPY package*.json ./
RUN npm install

# Copy source code
COPY . .

# Expose dev server port
EXPOSE 5173

# Start development server
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]

# Build stage
FROM node:20-alpine AS build_stage
WORKDIR /app

# Install dependencies
COPY package*.json ./
RUN npm install

# Build the static SvelteKit site
COPY . .
RUN npm run build

# Production stage - serve static files with nginx
FROM nginx:alpine AS production
WORKDIR /app

# Copy built static files to nginx html directory
COPY --from=build_stage /app/build /usr/share/nginx/html

# Copy custom nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Expose port 80
EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]