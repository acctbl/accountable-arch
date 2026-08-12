# syntax=docker/dockerfile:1.7

FROM ubuntu:24.04

ARG TARGETARCH
ARG MISE_VERSION=2026.7.13

USER root

RUN apt-get update \
	&& apt-get install --yes --no-install-recommends \
		bash \
		build-essential \
		ca-certificates \
		curl \
		docker.io \
		fonts-freefont-ttf \
		fonts-ipafont-gothic \
		git \
		jq \
		sudo \
		tar \
		unzip \
		xz-utils \
		fonts-liberation \
		fonts-noto-color-emoji \
		fonts-tlwg-loma-otf \
		fonts-unifont \
		fonts-wqy-zenhei \
		libasound2t64 \
		libatomic1 \
		libatk-bridge2.0-0t64 \
		libatk1.0-0t64 \
		libatspi2.0-0t64 \
		libcairo2 \
		libcups2t64 \
		libdbus-1-3 \
		libdrm2 \
		libfontenc1 \
		libgbm1 \
		libglib2.0-0t64 \
		libgl1 \
		libgl1-mesa-dri \
		libglvnd0 \
		libglx-mesa0 \
		libglx0 \
		libice6 \
		libnspr4 \
		libnss3 \
		libpango-1.0-0 \
		libsm6 \
		libunwind8 \
		libvulkan1 \
		libx11-6 \
		libxaw7 \
		libxcb1 \
		libxcb-glx0 \
		libxcomposite1 \
		libxdamage1 \
		libxext6 \
		libxfixes3 \
		libxfont2 \
		libxkbcommon0 \
		libxkbfile1 \
		libxmu6 \
		libxmuu1 \
		libxpm4 \
		libxrandr2 \
		libxt6t64 \
		libxxf86vm1 \
		x11-common \
		x11-xkb-utils \
		xauth \
		xfonts-cyrillic \
		xfonts-encodings \
		xfonts-scalable \
		xfonts-utils \
		xserver-common \
		xvfb \
		postgresql \
	&& rm -rf /var/lib/apt/lists/*

RUN apt-get update \
	&& apt-get install --yes --no-install-recommends nodejs npm \
	&& npx --yes playwright@1.61.1 install-deps \
	&& apt-get purge --yes nodejs npm \
	&& apt-get autoremove --yes \
	&& rm -rf /var/lib/apt/lists/* /root/.npm

RUN case "${TARGETARCH}" in \
		amd64) mise_arch=x64; mise_sha256=c906aa3e764f6a693beb4005d9a89edde7b4edb60d0733bbdccbbcee3b982081 ;; \
		arm64) mise_arch=arm64; mise_sha256=e3b767a80b2c37ae3fe6d06fcd463ee28a636565d43cdbfd6e578c66f308239e ;; \
		*) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
	esac \
	&& curl --fail --location --silent --show-error \
		--connect-timeout 15 --max-time 600 --retry 2 --retry-all-errors --retry-max-time 900 \
		--continue-at - \
		"https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/mise-v${MISE_VERSION}-linux-${mise_arch}.tar.xz" \
		--output /tmp/mise.tar.xz \
	&& echo "${mise_sha256}  /tmp/mise.tar.xz" | sha256sum --check \
	&& tar --extract --xz --file /tmp/mise.tar.xz --directory /tmp mise/bin/mise \
	&& install --mode=0755 /tmp/mise/bin/mise /usr/local/bin/mise \
	&& rm -r /tmp/mise /tmp/mise.tar.xz

ARG USER_UID=1000
ARG USER_GID=1000

RUN if ! getent group "${USER_GID}" >/dev/null; then groupadd --gid "${USER_GID}" devcontainer; fi \
	&& existing_user="$(getent passwd "${USER_UID}" | cut --delimiter=: --fields=1)" \
	&& if [ -n "${existing_user}" ]; then \
		usermod --login devcontainer --home /home/devcontainer --move-home --shell /bin/bash "${existing_user}"; \
	else \
		useradd --uid "${USER_UID}" --gid "${USER_GID}" --create-home --shell /bin/bash devcontainer; \
	fi \
	&& usermod --gid "${USER_GID}" devcontainer \
	&& echo 'devcontainer ALL=(root) NOPASSWD:ALL' > /etc/sudoers.d/devcontainer \
	&& chmod 0440 /etc/sudoers.d/devcontainer

USER devcontainer

ENV MISE_DATA_DIR=/home/devcontainer/.local/share/mise
ENV MISE_CONFIG_DIR=/home/devcontainer/.config/mise
ENV MISE_HTTP_DOWNLOAD_TIMEOUT=7200
ENV PLAYWRIGHT_OS_DEPS_INSTALLED=1
ENV PATH=/home/devcontainer/.local/share/mise/shims:${PATH}
