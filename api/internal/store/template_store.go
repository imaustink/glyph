package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/glyph/api/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type pgTemplateStore struct{ pool DBPool }

func NewTemplateStore(pool DBPool) TemplateStore {
	return &pgTemplateStore{pool: pool}
}

const templateColumns = `id, user_id, name, content, title_template, todo_trigger, default_folder_id, is_default, org_id, is_private, created_at, updated_at`

// templateAccessFilter enforces the three-tier access policy for templates.
// See store.ResourceAccessFilter for the policy definition.
var templateAccessFilter = ResourceAccessFilter(ResourceTemplate)

func scanTemplate(row interface{ Scan(...interface{}) error }) (*model.Template, error) {
	t := &model.Template{}
	var triggerJSON []byte
	if err := row.Scan(
		&t.ID, &t.UserID, &t.Name, &t.Content, &t.TitleTemplate,
		&triggerJSON, &t.DefaultFolderID, &t.IsDefault, &t.OrgID, &t.IsPrivate, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if triggerJSON != nil {
		t.TodoTrigger = &model.TodoTriggerConfig{}
		if err := unmarshalJSON(triggerJSON, t.TodoTrigger); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (s *pgTemplateStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Template, error) {
	q := `SELECT ` + templateColumns + ` FROM templates WHERE ` + templateAccessFilter + ` ORDER BY created_at ASC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("templates list: %w", err)
	}
	defer rows.Close()
	templates := make([]*model.Template, 0)
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("templates scan: %w", err)
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (s *pgTemplateStore) GetByID(ctx context.Context, id, userID uuid.UUID) (*model.Template, error) {
	q := `SELECT ` + templateColumns + ` FROM templates WHERE ` + templateAccessFilter + ` AND id = $2`
	return scanTemplate(s.pool.QueryRow(ctx, q, userID, id))
}

func (s *pgTemplateStore) Upsert(ctx context.Context, t *model.Template) (*model.Template, error) {
	triggerJSON, err := marshalNullableJSON(t.TodoTrigger)
	if err != nil {
		return nil, err
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	q := `INSERT INTO templates (id, user_id, name, content, title_template, todo_trigger, default_folder_id, is_default, org_id, is_private)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		  ON CONFLICT (id) DO UPDATE SET
		    name = EXCLUDED.name,
		    content = EXCLUDED.content,
		    title_template = EXCLUDED.title_template,
		    todo_trigger = EXCLUDED.todo_trigger,
		    default_folder_id = EXCLUDED.default_folder_id,
		    is_default = EXCLUDED.is_default,
		    org_id = EXCLUDED.org_id,
		    is_private = EXCLUDED.is_private,
		    updated_at = NOW()
		  WHERE templates.user_id = $2
		  RETURNING ` + templateColumns
	result, err := scanTemplate(s.pool.QueryRow(ctx, q,
		t.ID, t.UserID, t.Name, t.Content, t.TitleTemplate, triggerJSON, t.DefaultFolderID, t.IsDefault, t.OrgID, t.IsPrivate,
	))
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *pgTemplateStore) Create(ctx context.Context, t *model.Template) (*model.Template, error) {
	triggerJSON, err := marshalNullableJSON(t.TodoTrigger)
	if err != nil {
		return nil, err
	}
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	q := `INSERT INTO templates (id, user_id, name, content, title_template, todo_trigger, default_folder_id, is_default, org_id, is_private)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		  RETURNING ` + templateColumns
	return scanTemplate(s.pool.QueryRow(ctx, q,
		t.ID, t.UserID, t.Name, t.Content, t.TitleTemplate, triggerJSON, t.DefaultFolderID, t.IsDefault, t.OrgID, t.IsPrivate,
	))
}

func (s *pgTemplateStore) Update(ctx context.Context, t *model.Template) (*model.Template, error) {
	triggerJSON, err := marshalNullableJSON(t.TodoTrigger)
	if err != nil {
		return nil, err
	}
	q := `UPDATE templates
		  SET name=$1, content=$2, title_template=$3, todo_trigger=$4, default_folder_id=$5, is_default=$6,
		      org_id=$7, is_private=$8, updated_at=NOW()
		  WHERE id=$9 AND user_id=$10
		  RETURNING ` + templateColumns
	return scanTemplate(s.pool.QueryRow(ctx, q,
		t.Name, t.Content, t.TitleTemplate, triggerJSON, t.DefaultFolderID, t.IsDefault,
		t.OrgID, t.IsPrivate, t.ID, t.UserID,
	))
}

func (s *pgTemplateStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM templates WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func unmarshalJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	return nil
}
