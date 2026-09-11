ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG HOST_UID=1000
ARG HOST_GID=1000
ARG BASE_DIGEST
ARG CONTEXT_DIGEST
ARG PACKAGE_SET_SHA256

LABEL org.fogcast.target-image.base-digest="${BASE_DIGEST}" \
      org.fogcast.target-image.context-digest="${CONTEXT_DIGEST}" \
      org.fogcast.target-image.package-set-sha256="${PACKAGE_SET_SHA256}" \
      org.fogcast.target-image.host-uid="${HOST_UID}" \
      org.fogcast.target-image.host-gid="${HOST_GID}"

ENV DEBIAN_FRONTEND=noninteractive

RUN rm -f /etc/apt/sources.list.d/debian.sources \
 && printf '%s\n' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260801T120000Z bookworm main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260801T120000Z bookworm-updates main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian-security/20260801T120000Z bookworm-security main' \
      > /etc/apt/sources.list \
 && apt-get update \
 && apt-get install --no-install-recommends -y \
      bc \
      bison \
      build-essential \
      ca-certificates \
      cpio \
      device-tree-compiler \
      e2fsprogs \
      file \
      flex \
      git \
      libelf-dev \
      libncurses-dev \
      libssl-dev \
      lz4 \
      patch \
      perl \
      python3 \
      qemu-system-arm \
      qemu-user-static \
      rsync \
      unzip \
      wget \
      xz-utils \
 && rm -rf /var/lib/apt/lists/*

RUN actual="$(dpkg-query -W -f='${binary:Package}=${Version}\n' | LC_ALL=C sort | sha256sum | awk '{print $1}')" \
 && { test "${actual}" = "${PACKAGE_SET_SHA256}" \
      || { echo "package-set digest is ${actual}" >&2; exit 1; }; }

COPY containers/target-image/create-builder-user.sh /usr/local/sbin/create-builder-user
RUN HOST_UID="${HOST_UID}" HOST_GID="${HOST_GID}" /usr/local/sbin/create-builder-user \
 && rm /usr/local/sbin/create-builder-user

RUN mkdir -p /target-image-output \
 && chown "${HOST_UID}:${HOST_GID}" /target-image-output

USER builder
WORKDIR /work
