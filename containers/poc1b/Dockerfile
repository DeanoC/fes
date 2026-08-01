ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG HOST_UID=1000
ARG HOST_GID=1000

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
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

COPY containers/poc1b/create-builder-user.sh /usr/local/sbin/create-builder-user
RUN HOST_UID="${HOST_UID}" HOST_GID="${HOST_GID}" /usr/local/sbin/create-builder-user \
 && rm /usr/local/sbin/create-builder-user

USER builder
WORKDIR /work
