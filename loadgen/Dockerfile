FROM alpine:3.22
RUN apk add --no-cache wget
COPY decide.sh loop.sh /opt/
RUN chmod 755 /opt/decide.sh /opt/loop.sh
ENTRYPOINT ["/opt/loop.sh"]
