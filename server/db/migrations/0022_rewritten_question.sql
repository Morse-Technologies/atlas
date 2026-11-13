-- A follow-up question ("and the pressure?") is searched for as a standalone question; the text that was really
-- searched for is kept with the answer, so what the person sees ("Searched for: ...") can be traced.

-- +goose Up
ALTER TABLE search.answers ADD COLUMN rewritten_question text;

-- +goose Down
ALTER TABLE search.answers DROP COLUMN rewritten_question;
