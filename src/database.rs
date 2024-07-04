use rusqlite::{params, Connection, Result};
use std::sync::{Arc, Mutex};

const MINIMAL_VERSION: &str = "0.1";
const LATEST_VERSION: &str = "0.1";

pub struct OneTimeShareDb {
    conn: Arc<Mutex<Connection>>,
}

impl OneTimeShareDb {
    pub fn connect(path: &str) -> Result<Self> {
        //let conn = Connection::open(path)?;
        let conn = Connection::open_in_memory()?;
        let db = OneTimeShareDb {
            conn: Arc::new(Mutex::new(conn)),
        };
        db.init()?;
        Ok(db)
    }

    fn init(&self) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "CREATE TABLE IF NOT EXISTS global_vars (
                name TEXT PRIMARY KEY,
                integer_value INTEGER,
                string_value TEXT
            )",
            [],
        )?;

        conn.execute(
            "CREATE TABLE IF NOT EXISTS users (
                id INTEGER PRIMARY KEY,
                token TEXT NOT NULL UNIQUE,
                retention_limit_minutes INTEGER NOT NULL,
                max_size_bytes INTEGER NOT NULL,
                message_creation_limit_minutes INTEGER NOT NULL,
                last_message_creation_timestamp INTEGER
            )",
            [],
        )?;

        conn.execute(
            "CREATE TABLE IF NOT EXISTS messages (
                id INTEGER PRIMARY KEY,
                message_token TEXT NOT NULL UNIQUE,
                expire_timestamp INTEGER NOT NULL,
                data TEXT NOT NULL
            )",
            [],
        )?;

        conn.execute("CREATE INDEX IF NOT EXISTS token_index ON users(token)", [])?;
        conn.execute(
            "CREATE INDEX IF NOT EXISTS message_token_index ON messages(message_token)",
            [],
        )?;

        Ok(())
    }

    pub fn get_database_version(&self) -> Result<String> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT string_value FROM global_vars WHERE name='version'")?;
        let version = stmt
            .query_row([], |row| row.get(0))
            .unwrap_or_else(|_| LATEST_VERSION.to_string());
        Ok(version)
    }

    pub fn set_database_version(&self, version: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM global_vars WHERE name='version'", [])?;
        conn.execute(
            "INSERT INTO global_vars (name, string_value) VALUES ('version', ?1)",
            params![version],
        )?;
        Ok(())
    }

    pub fn set_user_limits(
        &self,
        token: &str,
        retention_limit_minutes: i32,
        max_size_bytes: i32,
        message_creation_limit_minutes: i32,
    ) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO users (token, retention_limit_minutes, max_size_bytes, message_creation_limit_minutes) VALUES (?1, ?2, ?3, ?4)
            ON CONFLICT(token) DO UPDATE SET retention_limit_minutes=?2, max_size_bytes=?3, message_creation_limit_minutes=?4",
            params![token, retention_limit_minutes, max_size_bytes, message_creation_limit_minutes],
        )?;
        Ok(())
    }

    pub fn get_user_limits(&self, token: &str) -> Result<(bool, u32, u32, u32)> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT retention_limit_minutes, max_size_bytes, message_creation_limit_minutes FROM users WHERE token=?1")?;
        let mut rows = stmt.query(params![token])?;
        if let Some(row) = rows.next()? {
            Ok((true, row.get(0)?, row.get(1)?, row.get(2)?))
        } else {
            Ok((false, 0, 0, 0))
        }
    }

    pub fn set_user_last_message_creation_time(&self, token: &str, timestamp: i64) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "UPDATE users SET last_message_creation_timestamp=?1 WHERE token=?2",
            params![timestamp, token],
        )?;
        Ok(())
    }

    pub fn get_user_last_message_creation_time(&self, token: &str) -> Result<i64> {
        let conn = self.conn.lock().unwrap();
        let mut stmt =
            conn.prepare("SELECT last_message_creation_timestamp FROM users WHERE token=?1")?;
        let timestamp = stmt
            .query_row(params![token], |row| row.get(0))
            .unwrap_or(0);
        Ok(timestamp)
    }

    pub fn save_message(
        &self,
        message_token: &str,
        expire_timestamp: i64,
        data: &str,
    ) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO messages (message_token, expire_timestamp, data) VALUES (?1, ?2, ?3)",
            params![message_token, expire_timestamp, data],
        )?;
        Ok(())
    }

    pub fn try_consume_message(&self, message_token: &str) -> Result<(Option<String>, i64)> {
        let conn = self.conn.lock().unwrap();
        let mut stmt =
            conn.prepare("SELECT id, data, expire_timestamp FROM messages WHERE message_token=?1")?;
        let mut rows = stmt.query(params![message_token])?;
        if let Some(row) = rows.next()? {
            let id: i32 = row.get(0)?;
            let data: String = row.get(1)?;
            let expire_timestamp: i64 = row.get(2)?;
            conn.execute("DELETE FROM messages WHERE id=?1", params![id])?;
            Ok((Some(data), expire_timestamp))
        } else {
            Ok((None, 0))
        }
    }

    pub fn remove_user_by_token(&self, token: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM users WHERE token=?1", params![token])?;
        Ok(())
    }

    pub fn clear_expired_messages(&self, limit_timestamp: i64) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "DELETE FROM messages WHERE expire_timestamp<?1",
            params![limit_timestamp],
        )?;
        Ok(())
    }
}

pub fn update_version(db: &OneTimeShareDb) -> Result<()> {
    let current_version = db.get_database_version()?;
    if current_version != LATEST_VERSION {
        let updaters = make_updaters(&current_version, LATEST_VERSION);
        for updater in updaters {
            updater.update_db(db)?;
        }
    }
    db.set_database_version(LATEST_VERSION)?;
    Ok(())
}

fn make_updaters(version_from: &str, version_to: &str) -> Vec<DbUpdater> {
    let all_updaters = make_all_updaters();
    let mut updaters = Vec::new();
    let mut is_first_found = version_from == MINIMAL_VERSION;

    for updater in all_updaters {
        if is_first_found {
            updaters.push(updater.clone());
            if updater.version == version_to {
                break;
            }
        } else if updater.version == version_from {
            is_first_found = true;
        }
    }

    if !updaters.is_empty() && updaters.last().unwrap().version != version_to {
        panic!(
            "Last version updater not found. Expected: {} Found: {}",
            version_to,
            updaters.last().unwrap().version
        );
    }

    updaters
}

fn make_all_updaters() -> Vec<DbUpdater> {
    vec![]
}

#[derive(Clone)]
struct DbUpdater {
    version: &'static str,
    update_db: fn(&OneTimeShareDb) -> Result<()>,
}

impl DbUpdater {
    pub fn update_db(&self, db: &OneTimeShareDb) -> Result<()> {
        (self.update_db)(db)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    fn setup_db() -> OneTimeShareDb {
        let temp_file = NamedTempFile::new().unwrap();
        OneTimeShareDb::connect(temp_file.path().to_str().unwrap()).unwrap()
    }

    #[test]
    fn test_get_and_set_database_version() {
        let db = setup_db();
        assert_eq!(db.get_database_version().unwrap(), LATEST_VERSION);

        db.set_database_version("0.2").unwrap();
        assert_eq!(db.get_database_version().unwrap(), "0.2");
    }

    #[test]
    fn test_set_and_get_user_limits() {
        let db = setup_db();
        db.set_user_limits("user1", 60, 1024, 5).unwrap();

        let (found, retention, max_size, creation_limit) = db.get_user_limits("user1").unwrap();
        assert!(found);
        assert_eq!(retention, 60);
        assert_eq!(max_size, 1024);
        assert_eq!(creation_limit, 5);
    }

    #[test]
    fn test_set_and_get_user_last_message_creation_time() {
        let db = setup_db();
        db.set_user_limits("user1", 60, 1024, 5).unwrap();

        db.set_user_last_message_creation_time("user1", 12345)
            .unwrap();
        let timestamp = db.get_user_last_message_creation_time("user1").unwrap();
        assert_eq!(timestamp, 12345);
    }

    #[test]
    fn test_save_and_consume_message() {
        let db = setup_db();
        db.save_message("token1", 12345, "Hello, world!").unwrap();

        let (data, expire) = db.try_consume_message("token1").unwrap();
        assert_eq!(data.unwrap(), "Hello, world!");
        assert_eq!(expire, 12345);

        let (data, _expire) = db.try_consume_message("token1").unwrap();
        assert!(data.is_none());
    }

    #[test]
    fn test_clear_expired_messages() {
        let db = setup_db();
        db.save_message("token1", 100, "Hello, world!").unwrap();
        db.save_message("token2", 200, "Hello, again!").unwrap();

        db.clear_expired_messages(160).unwrap();
        let (data, _expire) = db.try_consume_message("token1").unwrap();
        assert!(data.is_none());

        let (data, _expire) = db.try_consume_message("token2").unwrap();
        assert_eq!(data.unwrap(), "Hello, again!");
    }

    #[test]
    fn test_remove_user_limits() {
        let db = setup_db();
        db.set_user_limits("user1", 60, 1024, 5).unwrap();
        assert!(db.get_user_limits("user1").unwrap().0);

        db.set_user_limits("user2", 60, 1024, 5).unwrap();
        assert!(db.get_user_limits("user2").unwrap().0);

        db.remove_user_by_token("user1").unwrap();
        assert!(!db.get_user_limits("user1").unwrap().0);

        db.remove_user_by_token("user2").unwrap();
        assert!(!db.get_user_limits("user2").unwrap().0);
    }

    #[test]
    fn test_update_user_limits() {
        let db = setup_db();
        let token = "321";

        db.set_user_limits(token, 1, 2, 3).unwrap();
        {
            let (is_found, retention_limit_minutes, max_size_bytes, creation_limit_minutes) =
                db.get_user_limits(token).unwrap();
            assert!(is_found);
            assert_eq!(retention_limit_minutes, 1);
            assert_eq!(max_size_bytes, 2);
            assert_eq!(creation_limit_minutes, 3);
        }

        db.set_user_limits(token, 4, 5, 6).unwrap();
        {
            let (is_found, retention_limit_minutes, max_size_bytes, creation_limit_minutes) =
                db.get_user_limits(token).unwrap();
            assert!(is_found);
            assert_eq!(retention_limit_minutes, 4);
            assert_eq!(max_size_bytes, 5);
            assert_eq!(creation_limit_minutes, 6);
        }
    }

    #[test]
    fn test_user_last_message_creation_time() {
        let db = setup_db();
        let token = "123";

        db.set_user_limits(token, 1, 2, 3).unwrap();

        {
            let last_time = db.get_user_last_message_creation_time(token).unwrap();
            assert_eq!(last_time, 0);
        }

        {
            db.set_user_last_message_creation_time(token, 100).unwrap();
            let last_time = db.get_user_last_message_creation_time(token).unwrap();
            assert_eq!(last_time, 100);
        }

        {
            db.set_user_last_message_creation_time(token, 200).unwrap();
            let last_time = db.get_user_last_message_creation_time(token).unwrap();
            assert_eq!(last_time, 200);
        }
    }

    #[test]
    fn test_setting_limits_does_not_change_last_message_creation_time() {
        let db = setup_db();
        let token = "123";

        db.set_user_limits(token, 0, 0, 0).unwrap();
        db.set_user_last_message_creation_time(token, 100).unwrap();
        assert_eq!(db.get_user_last_message_creation_time(token).unwrap(), 100);

        db.set_user_limits(token, 1, 2, 3).unwrap();
        assert_eq!(db.get_user_last_message_creation_time(token).unwrap(), 100);
    }
}
