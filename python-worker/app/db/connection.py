import psycopg
from psycopg.rows import dict_row
from app.config import Config

class DBConnection:
    @staticmethod
    def get_connection():
        """Returns a new blocking database connection."""
        conn = psycopg.connect(Config.DATABASE_URL, row_factory=dict_row)
        return conn

    @staticmethod
    def execute_query(query: str, params: tuple = None, fetch: bool = False):
        """Helper to run simple queries without manually managing connections."""
        with DBConnection.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, params)
                if fetch:
                    return cur.fetchall()
                conn.commit()
                return None
