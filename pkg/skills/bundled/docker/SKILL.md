---
name: docker
description: "Container management with Docker and Docker Compose — build, run, manage containers and images"
require_bins: ["docker"]
---

# Docker

## When to Use
- Building and running containers
- Managing images, volumes, networks
- Docker Compose for multi-container applications
- Inspecting container logs and status
- Cleaning up unused resources

## Common Commands

### Containers
```
docker ps                               # List running containers
docker ps -a                            # List all containers (including stopped)
docker run -d --name myapp -p 8080:80 nginx  # Run detached with port mapping
docker run --rm -it ubuntu bash         # Interactive shell, auto-remove on exit
docker stop <container>                 # Stop a container
docker rm <container>                   # Remove a container
docker logs <container>                 # View container logs
docker logs -f <container>              # Follow logs in real-time
docker exec -it <container> bash        # Shell into running container
docker inspect <container>              # Detailed container info (JSON)
```

### Images
```
docker images                           # List local images
docker build -t myapp:latest .          # Build from Dockerfile
docker build -t myapp:latest -f Dockerfile.prod .  # Custom Dockerfile
docker pull nginx:alpine               # Pull an image
docker push myregistry/myapp:latest    # Push to registry
docker rmi <image>                     # Remove an image
```

### Docker Compose
```
docker compose up -d                    # Start services in background
docker compose down                     # Stop and remove containers
docker compose logs -f                  # Follow logs for all services
docker compose ps                       # List running services
docker compose exec <service> bash      # Shell into a service
docker compose build                    # Rebuild images
docker compose pull                     # Pull latest images
```

### Cleanup
```
docker system prune                     # Remove unused data
docker system prune -a                  # Remove ALL unused data (including images)
docker volume prune                     # Remove unused volumes
docker image prune                      # Remove dangling images
```

### Volumes & Networks
```
docker volume ls                        # List volumes
docker volume create mydata             # Create a volume
docker network ls                       # List networks
docker network create mynet             # Create a network
```

## Tips
- Use `--format` for custom output: `docker ps --format "table {{.Names}}\t{{.Status}}"`
- Use `docker compose` (v2) not `docker-compose` (v1, deprecated)
- Always use `-d` for background services and `--rm` for one-off commands
