# Each upstream image is pinned; building never requires package installation.
FROM node@sha256:de170b39439eb0e5a3444ad7d60b015614a381e10fecae2e30ec9e735122f38d AS node
FROM golang@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af AS golang
FROM python@sha256:21206b8f02efda6d737e402e1c716b650ad30605843a36e4bcb55463a7ebdf37
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=golang /usr/local/go /usr/local/go
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm && ln -s /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
# Provisioner verifies this exact public tarball before entering the build context:
# typescript5.9.3 sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw==
COPY typescript.tgz /tmp/typescript.tgz
RUN npm install --global --offline --ignore-scripts --no-audit --no-fund /tmp/typescript.tgz && rm /tmp/typescript.tgz
ENV PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
LABEL org.opencontainers.image.title="Gemma Desktop isolated coding toolchain"
