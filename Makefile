run:
	docker-compose up -d
up: run

down:
	docker-compose down


# Build the PII interceptor plugin inside a docker container using matching workspace and Go version
build-plugin:
	docker run --rm \
		-v $$(pwd):/workspace \
		-w /workspace/external/plugin/interception \
		golang:1.26.2-alpine3.23@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 \
		sh -c "apk add --no-cache gcc musl-dev binutils-gold && \
			CGO_ENABLED=1 go build \
				-buildmode=plugin \
				-a -trimpath \
				-tags \"sqlite_static\" \
				-o /workspace/data/plugins/pii-interceptor.so \
				main.go"

# Build the custom dynamically-linked Bifrost Docker image
build-bifrost-image:
	rm -rf external/bifrost/plugins/interception
	mkdir -p external/bifrost/plugins/interception
	cp -R external/plugin/interception/* external/bifrost/plugins/interception/
	docker build --build-arg VERSION=1.5.5 -t custom-bifrost:latest -f external/bifrost/transports/Dockerfile.local external/bifrost


