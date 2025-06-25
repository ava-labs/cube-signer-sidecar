FROM debian:12-slim

# Ensure all packages are up-to-date and patched
RUN apt-get update && \
	apt-get dist-upgrade -y && \
	apt-get install -y --no-install-recommends ca-certificates && \
	rm -rf /var/lib/apt/lists/*
COPY cubist-signer /usr/bin/cubist-signer
EXPOSE 8080
USER 1001
CMD ["start"]
ENTRYPOINT [ "/usr/bin/cubist-signer" ]
