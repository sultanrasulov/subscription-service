BEGIN;

-- A month is stored as a date on the 1st of that month (design §5.1).
-- service_name uses the built-in pg_c_utf8 collation, so lower() handles
-- Cyrillic the same way whatever locale the database was created with (§5.2).
CREATE TABLE subscriptions (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL,
    service_name text        COLLATE pg_c_utf8 NOT NULL,
    price        integer     NOT NULL,
    start_date   date        NOT NULL,
    end_date     date,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_service_name_length
        CHECK (char_length(service_name) BETWEEN 1 AND 100),
    CONSTRAINT subscriptions_price_non_negative
        CHECK (price >= 0),
    CONSTRAINT subscriptions_start_date_month
        CHECK (extract(day FROM start_date) = 1
               AND start_date BETWEEN DATE '2000-01-01' AND DATE '2100-12-01'),
    CONSTRAINT subscriptions_end_date_month
        CHECK (end_date IS NULL
               OR (extract(day FROM end_date) = 1
                   AND end_date BETWEEN DATE '2000-01-01' AND DATE '2100-12-01')),
    CONSTRAINT subscriptions_end_not_before_start
        CHECK (end_date IS NULL OR end_date >= start_date)
);

CREATE INDEX subscriptions_user_id_idx ON subscriptions (user_id);
CREATE INDEX subscriptions_service_name_lower_idx ON subscriptions (lower(service_name));

COMMIT;
