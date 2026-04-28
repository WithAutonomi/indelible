-- +goose Up

INSERT INTO settings (key, value) VALUES
    ('antd_quote_timeout_secs', '300'),
    ('antd_health_probe_timeout_secs', '15')
ON CONFLICT(key) DO NOTHING;

-- +goose Down

DELETE FROM settings WHERE key IN ('antd_quote_timeout_secs', 'antd_health_probe_timeout_secs');
