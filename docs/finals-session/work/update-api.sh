set -eu
systemctl stop cleanup-github.service
runuser -l root -c 'cd /opt/cleanup/repo && git fetch origin aws-github-finals && git checkout --detach FETCH_HEAD && bash scripts/api-build.sh && API_IMAGE=$(cat .build/api-image-id) docker compose -f compose.api.yaml up -d --force-recreate api gateway && API_IMAGE=$(cat .build/api-image-id) docker compose -f compose.api.yaml run --rm --no-deps deliver /usr/local/bin/deliver --retry-rejected all' > /opt/cleanup/api-update.log 2>&1
systemctl start cleanup-github.service
