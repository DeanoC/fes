FROM docker.io/library/debian:12.11-slim@sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef
ARG BUILDER_UID
ARG BUILDER_GID
ARG CONTEXT_SHA256
ARG PACKAGES_SHA256
RUN printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20250702T000000Z bookworm main\ndeb [check-valid-until=no] http://snapshot.debian.org/archive/debian-security/20250702T000000Z bookworm-security main\n' > /etc/apt/sources.list \
    && rm /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends dosfstools e2fsprogs mtools python3 \
    && rm -rf /var/lib/apt/lists/*
COPY packages.sha256 create-builder-user.sh /opt/boot-media/
RUN python3 -c 'import hashlib,pathlib,subprocess; data=subprocess.check_output(["dpkg-query","-W","-f=${Package}\t${Version}\t${Architecture}\n"]); actual=hashlib.sha256(b"".join(sorted(data.splitlines(keepends=True)))).hexdigest(); expected=pathlib.Path("/opt/boot-media/packages.sha256").read_text().strip(); assert actual == expected, (actual,expected)' \
    && test "$(cat /opt/boot-media/packages.sha256)" = "$PACKAGES_SHA256" \
    && sh /opt/boot-media/create-builder-user.sh "$BUILDER_UID" "$BUILDER_GID"
LABEL org.fes.media.base="docker.io/library/debian:12.11-slim@sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef" \
      org.fes.media.context-sha256="$CONTEXT_SHA256" \
      org.fes.media.packages-sha256="$PACKAGES_SHA256" \
      org.fes.media.uid="$BUILDER_UID" \
      org.fes.media.gid="$BUILDER_GID"
ENV TZ=UTC SOURCE_DATE_EPOCH=1751459412 LC_ALL=C
USER builder
WORKDIR /work
ENTRYPOINT ["python3", "/work/scripts/media_inside.py"]
