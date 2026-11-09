FROM st1528-conv
USER root
RUN apt-get update && apt-get install -y --no-install-recommends xvfb xdotool x11-utils imagemagick && rm -rf /var/lib/apt/lists/*
