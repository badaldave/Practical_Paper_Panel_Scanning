import psycopg
from app.config import Config
from psycopg.rows import dict_row

with psycopg.connect(Config.DATABASE_URL, row_factory=dict_row) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT id, name, status, progress_percentage FROM documents WHERE id = '00000000-0000-0000-0000-000000000001'")
        row = cur.fetchone()
        print("Document Status in DB:", row)
