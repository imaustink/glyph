ALTER TABLE templates ADD COLUMN default_folder_id UUID REFERENCES pages(id) ON DELETE SET NULL;
