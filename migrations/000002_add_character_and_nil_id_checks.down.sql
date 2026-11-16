BEGIN;

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_user_id_not_nil,
    DROP CONSTRAINT subscriptions_id_not_nil,
    DROP CONSTRAINT subscriptions_service_name_no_control;

COMMIT;
