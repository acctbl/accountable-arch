FROM grafana/k6@sha256:9bd01d6941fca969cb61bb57d2da5ee9b385fe2aa8881df3798c196564d6ace6

COPY --chown=12345:12345 tests/load/observability.js /scripts/observability.js

USER 12345:12345

ENTRYPOINT ["k6"]
CMD ["run", "/scripts/observability.js"]
