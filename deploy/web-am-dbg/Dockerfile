FROM archlinux:latest

# OS

RUN pacman -Sy --noconfirm go git ca-certificates \
     ttyd go-task zellij xclip xsel
RUN mkdir -p /app
RUN cp /usr/bin/go-task /usr/bin/task
RUN mkdir -p ~/.config/zellij/
RUN echo "show_startup_tips false" > ~/.config/zellij/config.kdl
RUN echo "show_release_notes false" >> ~/.config/zellij/config.kdl

# DEPS

COPY ./cview /app/cview
COPY ./asyncmachine-go/go.mod /app/asyncmachine-go/
COPY ./asyncmachine-go/go.sum /app/asyncmachine-go/

# PREBUILD

WORKDIR /app/asyncmachine-go
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

# SOURCE

COPY ./asyncmachine-go /app/asyncmachine-go
RUN --mount=type=cache,target=/root/.cache/go-build \
  task build-am-dbg
RUN --mount=type=cache,target=/root/.cache/go-build \
  task build-arpc

# START

# ports: am-dbg ttyd
EXPOSE 6831 7681
ENTRYPOINT ["go-task", "web-dashboard-repl"]
