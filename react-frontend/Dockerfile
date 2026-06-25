# Stage 1: Build
FROM node:20-alpine AS builder

WORKDIR /app

# Copy dependency specifications
COPY package.json ./
RUN npm install

# Copy source code
COPY . .

# Build Vite application
RUN npm run build

# Stage 2: Runtime
FROM nginx:1.25-alpine

# Copy built static assets from builder stage
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy custom Nginx configuration if needed (fallback to default)
EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
