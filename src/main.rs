use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};
use tide::{Request, Response, StatusCode};
use tide_rustls::TlsListener;
use uuid::Uuid;

mod database;
use crate::database::OneTimeShareDb;

#[derive(Clone)]
pub struct StaticData {
    default_index_html: String,
    shared_html: Vec<u8>,
    default_user_limits: UserLimits,
    config: Config,
    database: Arc<Mutex<OneTimeShareDb>>,
}

#[derive(Deserialize, Serialize, Clone)]
struct UserLimits {
    retention_limit_minutes: u32,
    max_message_size_bytes: u32,
    message_creation_limit_minutes: u32,
}

#[derive(Deserialize, Serialize, Clone)]
struct Config {
    port: String,
    database_path: String,
    force_unprotected_http: bool,
    cert_path: String,
    key_path: String,
    default_retention_limit_minutes: u32,
    default_max_message_size_bytes: u32,
    default_message_creation_limit_minutes: u32,
}

#[derive(Serialize, Deserialize)]
struct MessageForm {
    user_token: String,
    message_data: String,
    retention: Option<u32>,
}

async fn read_config(file_path: impl AsRef<Path>) -> tide::Result<Config> {
    let file_content = fs::read_to_string(file_path)?;
    let config: Config = serde_json::from_str(&file_content)?;
    Ok(config)
}

async fn home_page(req: Request<Arc<Mutex<StaticData>>>) -> tide::Result {
    let data = req.state().lock().unwrap();
    Ok(Response::builder(StatusCode::Ok)
        .body(data.default_index_html.clone())
        .build())
}

async fn create_new_message(mut req: Request<Arc<Mutex<StaticData>>>) -> tide::Result {
    if req.method() != http_types::Method::Post {
        return Ok(Response::builder(StatusCode::MethodNotAllowed)
            .body("Invalid request method")
            .build());
    }

    let form: MessageForm = req.body_form().await?;
    let retention_limit_minutes = form.retention.unwrap_or(0);

    let data = req.state().lock().unwrap();
    let (is_found, user_retention_limit_minutes, max_size_bytes, message_creation_limit_minutes) =
        data.database
            .lock()
            .unwrap()
            .get_user_limits(&form.user_token)?;

    if !is_found {
        return Ok(Response::builder(StatusCode::NotFound)
            .body("User not found")
            .build());
    }

    if message_creation_limit_minutes > 0 {
        let last_creation_time = data
            .database
            .lock()
            .unwrap()
            .get_user_last_message_creation_time(&form.user_token)?;
        if last_creation_time > 0 {
            let time_passed =
                SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs() as i64 - last_creation_time;
            if time_passed < (message_creation_limit_minutes as i64 * 60) {
                let minutes_left = message_creation_limit_minutes - (time_passed / 60) as u32;
                return Ok(Response::builder(StatusCode::BadRequest)
                    .body(format!(
                        "Message creation limit reached. Wait for {} minute(s) and repeat",
                        minutes_left
                    ))
                    .build());
            }
        }
    }

    if max_size_bytes > 0
        && STANDARD.decode(&form.message_data).unwrap().len() > max_size_bytes as usize
    {
        return Ok(Response::builder(StatusCode::BadRequest)
            .body("Message is too big")
            .build());
    }

    if retention_limit_minutes > 0
        && user_retention_limit_minutes > 0
        && retention_limit_minutes > user_retention_limit_minutes
    {
        return Ok(Response::builder(StatusCode::BadRequest)
            .body("Requested retention limit is bigger than allowed")
            .build());
    }

    data.database
        .lock()
        .unwrap()
        .set_user_last_message_creation_time(
            &form.user_token,
            SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs() as i64,
        )?;

    let message_token = Uuid::new_v4().to_string();
    let expire_timestamp = if retention_limit_minutes > 0 {
        SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs()
            + (retention_limit_minutes as u64 * 60)
    } else {
        0
    };

    data.database.lock().unwrap().save_message(
        &message_token,
        expire_timestamp as i64,
        &form.message_data,
    )?;

    let url_to_share = format!("https://{}/shared/{}", req.host().unwrap(), message_token);
    Ok(Response::builder(StatusCode::Ok).body(url_to_share).build())
}

async fn shared_page(req: Request<Arc<Mutex<StaticData>>>) -> tide::Result {
    if req.method() != http_types::Method::Get {
        return Ok(Response::builder(StatusCode::MethodNotAllowed)
            .body("Invalid request method")
            .build());
    }

    let token = req.url().path().trim_start_matches("/shared/");
    if token.is_empty() {
        return Ok(Response::builder(StatusCode::BadRequest)
            .body("Token is empty")
            .build());
    }

    let data = req.state().lock().unwrap();
    let html_response =
        String::from_utf8(data.shared_html.clone())?.replace("{{.MessageToken}}", token);

    Ok(Response::builder(StatusCode::Ok)
        .body(html_response)
        .build())
}

pub fn init_app(global_data: Arc<Mutex<StaticData>>) -> tide::Server<Arc<Mutex<StaticData>>> {
    let mut app = tide::with_state(global_data);

    app.at("/").get(home_page);
    app.at("/save").post(create_new_message);
    app.at("/shared/*").get(shared_page);

    app
}

#[async_std::main]
async fn main() -> tide::Result<()> {
    let config = read_config("app-config.json").await?;
    let default_user_limits = UserLimits {
        retention_limit_minutes: config.default_retention_limit_minutes,
        max_message_size_bytes: config.default_max_message_size_bytes,
        message_creation_limit_minutes: config.default_message_creation_limit_minutes,
    };

    let index_html = fs::read_to_string("index.html")?
        .replace(
            "{{.MessageLimitBytes}}",
            &default_user_limits.max_message_size_bytes.to_string(),
        )
        .replace(
            "{{.RetentionLimitMinutes}}",
            &default_user_limits.retention_limit_minutes.to_string(),
        );

    let shared_html = fs::read("shared.html")?;

    let database = OneTimeShareDb::connect(&config.database_path)?;

    database::update_version(&database)?;

    let global_data = Arc::new(Mutex::new(StaticData {
        default_index_html: index_html,
        shared_html,
        default_user_limits,
        config: config.clone(),
        database: Arc::new(Mutex::new(database)),
    }));

    let app = init_app(global_data);

    if config.force_unprotected_http {
        app.listen(format!("0.0.0.0:{}", config.port)).await?;
    } else {
        app.listen(
            TlsListener::build()
                .addrs(format!("0.0.0.0:{}", config.port))
                .cert(std::env::var("TIDE_CERT_PATH").unwrap())
                .key(std::env::var("TIDE_KEY_PATH").unwrap()),
        )
        .await?;
    }

    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Arc, Mutex};
    use tide::http::{Method, Request, Url};

    fn setup_test_data() -> Arc<Mutex<StaticData>> {
        let config = Config {
            port: "8080".to_string(),
            database_path: ":memory:".to_string(),
            force_unprotected_http: true,
            cert_path: "".to_string(),
            key_path: "".to_string(),
            default_retention_limit_minutes: 60,
            default_max_message_size_bytes: 1024,
            default_message_creation_limit_minutes: 5,
        };

        let default_user_limits = UserLimits {
            retention_limit_minutes: config.default_retention_limit_minutes,
            max_message_size_bytes: config.default_max_message_size_bytes,
            message_creation_limit_minutes: config.default_message_creation_limit_minutes,
        };

        let index_html = "<html>Index Page</html>".to_string();
        let shared_html = "<html>Shared Page with token {{.MessageToken}}</html>"
            .as_bytes()
            .to_vec();

        let database = OneTimeShareDb::connect(":memory:").unwrap();

        Arc::new(Mutex::new(StaticData {
            default_index_html: index_html,
            shared_html,
            default_user_limits,
            config,
            database: Arc::new(Mutex::new(database)),
        }))
    }

    #[async_std::test]
    async fn test_home_page() {
        let app_data = setup_test_data();
        let app = init_app(app_data.clone());

        let req = Request::new(Method::Get, Url::parse("http://localhost/").unwrap());
        let mut res: Response = app.respond(req).await.unwrap();
        let body = res.take_body().into_string().await.unwrap();

        assert_eq!(res.status(), StatusCode::Ok);
        assert_eq!(body, "<html>Index Page</html>");
    }

    #[async_std::test]
    async fn test_create_new_message() {
        let app_data = setup_test_data();
        let app = init_app(app_data.clone());

        // Insert a user into the database for testing
        let user_token = "test_token";
        app_data
            .lock()
            .unwrap()
            .database
            .lock()
            .unwrap()
            .set_user_limits(user_token, 60, 1024, 5)
            .unwrap();

        let mut req = Request::new(Method::Post, Url::parse("http://localhost/save").unwrap());
        req.set_body(
            tide::http::Body::from_form(&MessageForm {
                user_token: "test_token".to_string(),
                message_data: "SGVsbG8gd29ybGQ=".to_string(),
                retention: Some(60),
            })
            .unwrap(),
        );

        let res: Response = app.respond(req).await.unwrap();
        assert_eq!(res.status(), StatusCode::Ok);
    }

    #[async_std::test]
    async fn test_shared_page() {
        let app_data = setup_test_data();
        let app = init_app(app_data.clone());

        let token = "test_token";
        let url = format!("http://localhost/shared/{}", token);

        let req = Request::new(Method::Get, Url::parse(&url).unwrap());
        let mut res: Response = app.respond(req).await.unwrap();

        assert_eq!(res.status(), StatusCode::Ok);
        let body = res.take_body().into_string().await.unwrap();
        assert!(body.contains(token));
    }
}
