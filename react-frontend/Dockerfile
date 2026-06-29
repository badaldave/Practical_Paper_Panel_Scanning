# ==========================
# Stage 1 - Build
# ==========================
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
# Uses package-lock.json and ignores peer dependency conflicts
RUN npm ci --legacy-peer-deps

# Copy application source
COPY . .

# Build the Vite application
RUN npm run build

# ==========================
# Stage 2 - Production
# ==========================
FROM nginx:1.25-alpine

# Remove default nginx content
RUN rm -rf /usr/share/nginx/html/*

# Copy built application
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy custom nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
