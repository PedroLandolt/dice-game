CREATE TABLE api_tokens (
    token_hash BYTEA PRIMARY KEY,
    player_id  TEXT NOT NULL
);

CREATE TABLE plays (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id  TEXT NOT NULL,
    request_id TEXT NOT NULL,
    amount     BIGINT NOT NULL CHECK (amount > 0),
    bet_type   TEXT NOT NULL CHECK (bet_type IN ('even', 'odd')),
    rolled     INTEGER CHECK (rolled BETWEEN 1 AND 6),
    won        BOOLEAN,
    payout     BIGINT CHECK (payout >= 0),
    status     TEXT NOT NULL CHECK (status IN ('pending', 'open', 'closed', 'rejected')),
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at  TIMESTAMPTZ,
    CONSTRAINT plays_player_request_unique UNIQUE (player_id, request_id)
);

CREATE UNIQUE INDEX plays_one_active_per_player ON plays (player_id) WHERE status IN ('pending', 'open');
CREATE INDEX plays_active_created_at ON plays (created_at) WHERE status IN ('pending', 'open');

CREATE TABLE wallets (
    player_id TEXT PRIMARY KEY,
    balance   BIGINT NOT NULL CHECK (balance >= 0),
    currency  TEXT NOT NULL
);

CREATE TABLE wallet_transactions (
    tx_id         TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('debit', 'credit', 'rollback')),
    player_id     TEXT NOT NULL,
    amount        BIGINT NOT NULL CHECK (amount >= 0),
    balance_after BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, tx_id, kind)
);

CREATE TABLE ledger_entries (
    id            BIGINT GENERATED ALWAYS AS IDENTITY,
    player_id     TEXT NOT NULL,
    tx_id         TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('debit', 'credit', 'rollback', 'adjustment')),
    amount        BIGINT NOT NULL,
    balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at),
    CONSTRAINT ledger_entries_amount_sign CHECK (
        (kind = 'debit' AND amount < 0)
        OR (kind IN ('credit', 'rollback') AND amount > 0)
        OR kind = 'adjustment'
    )
) PARTITION BY RANGE (created_at);

CREATE INDEX ledger_entries_player_created ON ledger_entries (player_id, created_at DESC);
CREATE INDEX ledger_entries_created_brin ON ledger_entries USING BRIN (created_at);

CREATE TABLE ledger_entries_default PARTITION OF ledger_entries DEFAULT;

DO $$
DECLARE
    month_start DATE;
BEGIN
    FOR month_start IN
        SELECT generate_series(
            date_trunc('month', now()),
            date_trunc('month', now()) + INTERVAL '5 months',
            INTERVAL '1 month'
        )::date
    LOOP
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF ledger_entries FOR VALUES FROM (%L) TO (%L)',
            'ledger_entries_' || to_char(month_start, 'YYYY_MM'),
            month_start,
            month_start + INTERVAL '1 month'
        );
    END LOOP;
END $$;

CREATE ROLE dice_app LOGIN PASSWORD 'dice_app';
GRANT SELECT ON api_tokens TO dice_app;
GRANT SELECT, INSERT, UPDATE ON plays, wallets, wallet_transactions TO dice_app;
GRANT SELECT, INSERT ON ledger_entries TO dice_app;

INSERT INTO wallets (player_id, balance, currency) VALUES
    ('gandalf', 10000, 'EUR'),
    ('luffy', 500, 'EUR'),
    ('dante', 10000, 'EUR'),
    ('mr-robot', 10000, 'EUR');

INSERT INTO ledger_entries (player_id, tx_id, kind, amount, balance_after)
SELECT player_id, 'seed:' || player_id, 'adjustment', balance, balance FROM wallets;

INSERT INTO api_tokens (token_hash, player_id) VALUES
    (sha256('dev-gandalf'), 'gandalf'),
    (sha256('dev-luffy'), 'luffy'),
    (sha256('dev-dante'), 'dante'),
    (sha256('dev-mr-robot'), 'mr-robot');
