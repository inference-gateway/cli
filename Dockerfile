FROM alpine:3.22.1
RUN apk --no-cache add ca-certificates
RUN addgroup -g 1000 infer && \
    adduser -u 1000 -G infer -h /home/infer -s /bin/sh -D infer
WORKDIR /home/infer
ARG TARGETARCH
COPY --from=binaries infer-linux-${TARGETARCH} ./infer
RUN chmod +x ./infer && chown infer:infer ./infer
RUN mkdir -p .infer && chown -R infer:infer .infer
VOLUME ["/home/infer/.infer"]
USER infer
ENTRYPOINT ["./infer"]
