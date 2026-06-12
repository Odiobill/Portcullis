# Backup sidecar — Alpine + PostgreSQL client + dcron
FROM alpine:latest

RUN apk add --no-cache \
    postgresql-client \
    bash \
    tzdata

# Set timezone for cron schedule accuracy
ENV TZ=Europe/Rome

WORKDIR /app

COPY backup.sh retention.sh entrypoint.sh ./
RUN chmod +x backup.sh retention.sh entrypoint.sh

VOLUME ["/backups"]

ENTRYPOINT ["/bin/bash", "./entrypoint.sh"]
