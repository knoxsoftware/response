-- Ensure every existing admin has a corresponding responder row.
INSERT INTO responders (phone_number)
SELECT phone_number FROM admins
ON CONFLICT (phone_number) DO NOTHING;
