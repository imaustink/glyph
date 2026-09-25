-- A note's tasks belong to the note's owner, whoever typed the bullet (see
-- TaskHandler). Tasks a collaborator created on someone else's shared note
-- before this rule move to the note's owner; the collaborator still sees
-- and edits them through their access to the note.
UPDATE tasks t
SET user_id = p.user_id, updated_at = NOW()
FROM pages p
WHERE t.source_page_id = p.id
  AND t.user_id <> p.user_id;
