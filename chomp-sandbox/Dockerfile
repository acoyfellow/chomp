FROM docker.io/cloudflare/sandbox:0.7.0

# Install pi coding agent
RUN npm install -g @mariozechner/pi-coding-agent

# Install chomp CLI and agent runner
COPY bin/chomp /usr/local/bin/chomp
COPY bin/run-agent /usr/local/bin/run-agent
RUN chmod +x /usr/local/bin/chomp /usr/local/bin/run-agent
