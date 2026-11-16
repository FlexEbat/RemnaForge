package proxy

// Caddy configuration templates, moved out of the install flows unchanged.

// caddyDirectTemplate is the panel's Caddyfile, Caddy on 443. {$PANEL_DOMAIN},
// {$SUB_DOMAIN}, {$BACKEND_URL}, {$SUB_BACKEND_URL} are Caddy's own
// environment-variable placeholders (resolved by Caddy at runtime from
// the container environment set in the flow's compose file), so
// they stay as literal text here, same as caddynode's Caddyfile.
// cookiesRandom1/cookiesRandom2, by contrast, are substituted with Go's
// %s placeholders at file-creation time, since they need to be baked
// into the actual cookie name/value rather than resolved later.
const caddyDirectTemplate = `{
    admin off
}

http://{$PANEL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$PANEL_DOMAIN}{uri} permanent
}

https://{$PANEL_DOMAIN} {
    encode zstd gzip

    @has_token_param {
        query %[1]s=%[2]s
    }

    handle @has_token_param {
        header +Set-Cookie "%[1]s=%[2]s; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=2592000"
    }

    @unauthorized {
        not path /oauth2/*
        not header Cookie *%[1]s=%[2]s*
        not query %[1]s=%[2]s
    }

    handle @unauthorized {
        abort
    }

    @oauth2_bad {
        path /oauth2/*
        not header Referer https://oauth.telegram.org/*
    }

    handle @oauth2_bad {
        abort
    }

    @oauth2 {
        path /oauth2/*
        header Referer https://oauth.telegram.org/*
    }

    handle @oauth2 {
        reverse_proxy {$BACKEND_URL} {
            header_up Host {host}
        }
    }

    reverse_proxy {$BACKEND_URL} {
        header_up X-Real-IP {remote}
        header_up Host {host}
    }
}

https://{$SUB_DOMAIN} {
    encode zstd gzip
    handle {
        reverse_proxy {$SUB_BACKEND_URL} {
            header_up X-Real-IP {remote}
            header_up Host {host}
        }
    }
}

:80 {
    bind 0.0.0.0
    respond 204
}
`

// caddySocketTemplate is the panel+node Caddyfile, behind Xray on a unix socket. As in the node and panel-only
// templates, the {$VAR} domain references stay literal
// (resolved by Caddy from the container environment at runtime), while
// %[1]s/%[2]s substitute the actual generated cookie name/value at
// file-creation time.
const caddySocketTemplate = `{
    admin off
    servers {
        listener_wrappers {
            proxy_protocol
            tls
        }
    }
    auto_https disable_redirects
}

http://{$SELF_STEAL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$SELF_STEAL_DOMAIN}{uri} permanent
}

https://{$SELF_STEAL_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}

    handle /api/v2/stream-events {
        reverse_proxy unix//dev/shm/xhttp.sock {
            header_up Connection ""
        }
    }

    handle {
        root * /var/www/html
        try_files {path} /index.html
        file_server
    }
}

http://{$PANEL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$PANEL_DOMAIN}{uri} permanent
}

https://{$PANEL_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}
    encode zstd gzip

    @has_token_param {
        query %[1]s=%[2]s
    }

    handle @has_token_param {
        header +Set-Cookie "%[1]s=%[2]s; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=2592000"
    }

    @unauthorized {
        not path /oauth2/*
        not header Cookie *%[1]s=%[2]s*
        not query %[1]s=%[2]s
    }

    handle @unauthorized {
        root * /var/www/html
        try_files {path} /index.html
        file_server
    }

    @oauth2_bad {
        path /oauth2/*
        not header Referer https://oauth.telegram.org/*
    }

    handle @oauth2_bad {
        abort
    }

    @oauth2 {
        path /oauth2/*
        header Referer https://oauth.telegram.org/*
    }

    handle @oauth2 {
        reverse_proxy {$BACKEND_URL} {
            header_up Host {host}
        }
    }

    reverse_proxy {$BACKEND_URL} {
        header_up X-Real-IP {remote}
        header_up Host {host}
    }
}

http://{$SUB_DOMAIN} {
    bind 0.0.0.0
    redir https://{$SUB_DOMAIN}{uri} permanent
}

https://{$SUB_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}
    encode zstd gzip
    handle {
        reverse_proxy {$SUB_BACKEND_URL} {
            header_up X-Real-IP {remote}
            header_up Host {host}
        }
    }
}

:80 {
    bind 0.0.0.0
    respond 204
}
`

// caddyNodeTemplate is the node's Caddyfile.
// {$SELF_STEAL_DOMAIN} and {$CADDY_SOCKET_PATH} are Caddy's own
// environment-variable placeholders, resolved by Caddy itself at runtime
// from the container's environment (set in the flow's compose file),
// not by this Go code - the Caddyfile content is static, no domain
// substitution happens here.
const caddyNodeTemplate = `{
    admin off
    servers {
        listener_wrappers {
            proxy_protocol
            tls
        }
    }
    auto_https disable_redirects
}

http://{$SELF_STEAL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$SELF_STEAL_DOMAIN}{uri} permanent
}

https://{$SELF_STEAL_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}

    handle /api/v2/stream-events {
        reverse_proxy unix//dev/shm/xhttp.sock {
            header_up Connection ""
        }
    }

    handle {
        root * /var/www/html
        try_files {path} /index.html
        file_server
    }
}

:80 {
    bind 0.0.0.0
    respond 204
}
`
