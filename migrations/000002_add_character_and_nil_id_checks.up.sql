BEGIN;

-- Repeat two domain rules: a service name holds no control characters, and
-- neither ID is the nil UUID. Under this column's collation, [[:cntrl:]]
-- matches exactly the code points Go's unicode.IsControl does.
--
-- ADD CONSTRAINT checks the rows already stored, and one that broke a rule would
-- fail this migration. None can: no code wrote rows before this version.
ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_service_name_no_control
        CHECK (service_name !~ '[[:cntrl:]]'),
    ADD CONSTRAINT subscriptions_id_not_nil
        CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    ADD CONSTRAINT subscriptions_user_id_not_nil
        CHECK (user_id <> '00000000-0000-0000-0000-000000000000');

COMMIT;
