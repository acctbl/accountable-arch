-- +goose Up
-- +goose StatementBegin
CREATE TABLE public.accountable_proof_nonces (
    nonce uuid PRIMARY KEY,
    expires_at timestamptz NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE public.accountable_proof_nonces;
-- +goose StatementEnd
