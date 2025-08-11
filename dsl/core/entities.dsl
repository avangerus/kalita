
module core

entity User:
  name: string required
  email: string required unique
  role: enum[Manager, Admin, Developer, Designer] required

# Проект
entity Project:
  name: string required
  manager_id: ref[core.User] on_delete=set_null
  member_ids: array[ref[User]]          # массив ссылок на User
  tags: array[string]                   # массив строк
  budget: float
  start_date: date
  end_ts: datetime
  status: enum[Draft, InWork, Closed] required
  attachments: array[ref[core.Attachment]]    
  status: string catalog=ProjectStatus default=Draft
  description: string max_len=20 pattern=^[A-Za-z0-9 _-]+$
  budget: float min=0 max=1000000

entity Attachment:
  owner_entity: string
  owner_id: string
  file_name: string required
  mime: string
  size: int
  storage: enum["local","s3"] required default="local"
  storage_key: string required
  created_at: datetime
  constraints:
    unique(storage_key)


module test

entity Item:
  code: string required unique
  name: string
