FROM ubuntu@sha256:224a1869083a311ef3f13648a154ba79832fbef6364d31493642ca03082da254

RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates \
      git=1:2.43.0-1ubuntu7.3 \
      php-cli=2:8.3+93ubuntu2 \
      unzip=6.0-28ubuntu4.1 \
 && rm -rf /var/lib/apt/lists/*

ADD --checksum=sha256:f446ea719708bb85fcbf4ef18def5d0515f1f9b4d703f6d820c9c1656e10a2f2 \
    https://getcomposer.org/download/2.8.12/composer.phar /usr/local/bin/composer
COPY lifecycle.sh /opt/git-a2a-composer-lifecycle.sh

RUN chmod 0755 /usr/local/bin/composer /opt/git-a2a-composer-lifecycle.sh \
 && composer --version

ENV COMPOSER_ALLOW_SUPERUSER=1
ENTRYPOINT ["/opt/git-a2a-composer-lifecycle.sh"]
