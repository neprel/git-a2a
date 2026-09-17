FROM golang:1.25.1-bookworm@sha256:c423747fbd96fd8f0b1102d947f51f9b266060217478e5f9bf86f145969562ee

ARG TARGETARCH
ARG ZIG_VERSION=0.14.1

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl xz-utils; \
    rm -rf /var/lib/apt/lists/*; \
    case "$TARGETARCH" in \
      amd64) zig_arch=x86_64; zig_sha=24aeeec8af16c381934a6cd7d95c807a8cb2cf7df9fa40d359aa884195c4716c ;; \
      arm64) zig_arch=aarch64; zig_sha=f7a654acc967864f7a050ddacfaa778c7504a0eca8d2b678839c21eea47c992b ;; \
      *) echo "unsupported Docker architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 5 -o /tmp/zig.tar.xz "https://pkg.machengine.org/zig/zig-${zig_arch}-linux-${ZIG_VERSION}.tar.xz"; \
    echo "${zig_sha}  /tmp/zig.tar.xz" | sha256sum -c -; \
    mkdir /opt/zig; \
    tar -xJf /tmp/zig.tar.xz -C /opt/zig --strip-components=1; \
    ln -s /opt/zig/zig /usr/local/bin/zig; \
    rm /tmp/zig.tar.xz; \
    zig version; \
    go version; \
    git --version

WORKDIR /work/source

CMD ["sh", "-ec", "cp -a /workspace/. /work/source/ && go build -o /tmp/git-a2a ./cmd/git-a2a && GITA2A_IT_ZIG_CLI=1 GITA2A_CLI_BIN=/tmp/git-a2a go test ./adapters/zig -run '^TestPublicCLIZigLifecycle$' -count=1 -v"]
