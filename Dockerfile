FROM golang:1.11.5 AS builder
MAINTAINER stephane.martin_github@vesperal.eu
WORKDIR $GOPATH/src/github.com/stephane-martin/reaper
COPY . ./
RUN wget -O /bin/dep 'https://github.com/golang/dep/releases/download/v0.5.0/dep-linux-amd64'
RUN chmod a+x /bin/dep
RUN make reaper
RUN cp reaper /

FROM debian:stretch-slim
ENV TZ=Europe/Paris
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
RUN useradd --create-home --shell /bin/bash --home-dir /home/reaper --user-group --uid 502 reaper
COPY --from=builder --chown=reaper:reaper /reaper /home/reaper/
COPY tini /
RUN chmod a+x /tini
RUN chmod a+x /home/reaper/reaper

EXPOSE 1514/tcp

USER reaper
ENTRYPOINT ["/tini", "--"]
CMD ["/home/reaper/reaper","--tcp","127.0.0.1:1514"]
