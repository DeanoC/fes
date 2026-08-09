FROM debian@sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates coreutils file git make sed xz-utils \
    && rm -rf /var/lib/apt/lists/*

COPY gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz /tmp/arm-toolchain.tar.xz
RUN mkdir -p /opt/toolchain \
    && tar -xJf /tmp/arm-toolchain.tar.xz -C /opt/toolchain --strip-components=1 \
    && rm /tmp/arm-toolchain.tar.xz

ENV PATH=/opt/toolchain/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
