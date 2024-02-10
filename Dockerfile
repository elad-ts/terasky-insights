FROM debian:bullseye-slim

ARG TARGETVERSION
ARG TARGETARCH

# add a non-root 'steampipe' user
RUN adduser --system --disabled-login --ingroup 0 --gecos "steampipe user" --shell /bin/false --uid 9193 steampipe

# install python3 
RUN apt-get update && apt-get install -y python3 python3-pip && rm -rf /var/lib/apt/lists/*RUN

# updates and installs - 'wget' for downloading steampipe, 'less' for paging in 'steampipe query' interactive mode
RUN apt-get update -y && apt-get install -y curl wget unzip vim less && rm -rf /var/lib/apt/lists/*

# Install AWS CLI with support for x86_64 and aarch64
RUN if [ "$TARGETARCH" = "arm64" ]; then \
      curl "https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip" -o "awscliv2.zip"; \
    else \
      curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"; \
    fi && \
    unzip awscliv2.zip && ./aws/install

# # download the release as given in TARGETVERSION and TARGETARCH
RUN echo \
 && cd /tmp \
 && wget -nv https://github.com/turbot/steampipe/releases/download/${TARGETVERSION}/steampipe_linux_${TARGETARCH}.tar.gz \
 && tar xzf steampipe_linux_${TARGETARCH}.tar.gz \
 && mv steampipe /usr/local/bin/ \
 && rm -rf /tmp/steampipe_linux_${TARGETARCH}.tar.gz 

# Copy all mods
COPY mods /mods

# # Update permissions
RUN id steampipe
RUN mkdir -p /mods && chown -R steampipe:0 /mods && chmod -R 755 /mods

# Change user to non-root   
USER steampipe:0

# Use a constant workspace directory that can be mounted to
WORKDIR /mods/default

# disable auto-update
ENV STEAMPIPE_UPDATE_CHECK=false

ENV STEAMPIPE_DATABASE_START_TIMEOUT=300

# disable telemetry
ENV STEAMPIPE_TELEMETRY=none

# Steampipe install AWS plugin
RUN steampipe plugin install aws

# Create a temporary mod - this is required to make sure that the dashboard server starts without problems
RUN steampipe mod init

# Run steampipe service once
RUN steampipe service start --dashboard

# and stop it
RUN steampipe service stop

# Cleanup
# remove the generated service .passwd file from this image, so that it gets regenerated in the container
RUN rm -f /home/steampipe/.steampipe/internal/.passwd
RUN rm -rf /home/steampipe/.steampipe/logs

# remove the temporary mod
RUN rm -f ./mod.sp

# Install mods for each subdirectory
RUN for dir in /mods/*/; do cd "$dir" && steampipe mod install && cd -; done

COPY dashboard /home/steampipe/.steampipe/dashboard/assets
COPY generate_config_for_cross_account_roles.sh /home/steampipe

# # expose postgres service default port
EXPOSE 9193

# # expose dashboard service default port
EXPOSE 9194

COPY entrypoint.sh /usr/local/bin

ENTRYPOINT [ "entrypoint.sh" ]