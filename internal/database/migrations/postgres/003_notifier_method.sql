-- +goose Up

INSERT INTO settings (key, value) VALUES
    ('notifier_method', 'auto')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

DELETE FROM settings WHERE key = 'notifier_method';
