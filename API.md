# ShineCore Server API - Документация для клиентов

## Обзор

ShineCore Server API предоставляет доступ к манифесту и файлам (модам) для лаунчеров. API использует HMAC-SHA256 авторизацию для защиты запросов.

## Базовые параметры

- **BaseURL**: URL сервера (например, `https://api.be-sunshainy.ru`)
- **Secret**: Секретный ключ для подписи запросов (например, `sun`)
- **Timeout**: Рекомендуемое время жизни запроса - 5 минут (±5 минут от текущего времени)

## Авторизация (HMAC-SHA256)

Все запросы к защищённым эндпоинтам должны содержать подпись в заголовках HTTP:

### Заголовки:
- `X-Timestamp`: Unix timestamp (секунды)
- `X-Signature`: HMAC-SHA256 подпись запроса

### Алгоритм подписи:

```
message = METHOD + "\n" + PATH + "\n" + TIMESTAMP
signature = HMAC-SHA256(secret, message)
```

**Пример:**
```
Secret: "sun"
Method: "GET"
Path: "/manifest"
Timestamp: 1705482000

Message: "GET\n/manifest\n1705482000"
Signature: hex(HMAC-SHA256("sun", "GET\n/manifest\n1705482000"))
```

## Endpoints

### 1. Получить манифест

**GET** `/manifest`

Возвращает манифест с зависимостями и списком доступных файлов.

**Заголовки:**
```
X-Timestamp: 1705482000
X-Signature: abc123def456...
```

**Ответ (200 OK):**
```json
{
  "project": "ShineCore",
  "studio": "ShineCore-Studio",
  "version": "0.1.0",
  "generated_at": "2026-01-17T10:00:00Z",
  "dependencies": {
    "game_version": "1.21.1",
    "loader": "fabric",
    "loader_version": "0.18.4",
    "java_urls": {
      "java_8": "https://github.com/adoptium/temurin8-binaries/.../OpenJDK8U-jdk_x64_windows_hotspot_8u472b08.zip",
      "java_17": "https://github.com/adoptium/temurin17-binaries/.../OpenJDK17U-jdk_x64_windows_hotspot_17.0.9_9.zip",
      "java_21": "https://github.com/adoptium/temurin21-binaries/.../OpenJDK21U-jdk_x64_windows_hotspot_21.0.9_10.zip"
    }
  },
  "packages": {
    "mods": [
      {
        "path": "mods/example-mod.jar",
        "name": "example-mod.jar",
        "size": 123456,
        "sha256": "abc123...",
        "url": "/download/mods/mods/example-mod.jar"
      }
    ]
  }
}
```

**Ошибки:**
- `401 Unauthorized`: Неверная подпись или timestamp вне диапазона (±5 минут)
- `500 Internal Server Error`: Ошибка на сервере

### 2. Скачать файл мода

**GET** `/download/mods/{path}`

Скачивает файл мода по указанному пути из манифеста.

**Заголовки:**
```
X-Timestamp: 1705482000
X-Signature: abc123def456...
```

**Параметры:**
- `path`: Путь к файлу из манифеста (например, `mods/example-mod.jar`)

**Ответ (200 OK):**
Бинарный файл с заголовками:
- `Content-Type: application/octet-stream`
- `Content-Disposition: attachment; filename="example-mod.jar"`

**Ошибки:**
- `401 Unauthorized`: Неверная подпись
- `404 Not Found`: Файл не найден

## Примеры реализации

### Python

```python
import hmac
import hashlib
import time
import requests
from urllib.parse import urljoin

class ShineCoreClient:
    def __init__(self, base_url: str, secret: str):
        self.base_url = base_url.rstrip('/')
        self.secret = secret
    
    def _sign_request(self, method: str, path: str) -> dict:
        """Создаёт заголовки для подписи запроса"""
        timestamp = int(time.time())
        message = f"{method}\n{path}\n{timestamp}"
        signature = hmac.new(
            self.secret.encode(),
            message.encode(),
            hashlib.sha256
        ).hexdigest()
        return {
            "X-Timestamp": str(timestamp),
            "X-Signature": signature
        }
    
    def fetch_manifest(self):
        """Получает манифест с сервера"""
        path = "/manifest"
        headers = self._sign_request("GET", path)
        url = urljoin(self.base_url, path)
        response = requests.get(url, headers=headers)
        response.raise_for_status()
        return response.json()
    
    def download_mod(self, mod_path: str, save_to: str):
        """Скачивает мод по пути из манифеста"""
        path = f"/download/mods/{mod_path}"
        headers = self._sign_request("GET", path)
        url = urljoin(self.base_url, path)
        response = requests.get(url, headers=headers, stream=True)
        response.raise_for_status()
        with open(save_to, 'wb') as f:
            for chunk in response.iter_content(chunk_size=8192):
                f.write(chunk)

# Использование:
client = ShineCoreClient("https://api.be-sunshainy.ru", "sun")
manifest = client.fetch_manifest()
print(f"Game version: {manifest['dependencies']['game_version']}")
```

### JavaScript/Node.js

```javascript
const crypto = require('crypto');
const https = require('https');
const fs = require('fs');

class ShineCoreClient {
    constructor(baseURL, secret) {
        this.baseURL = baseURL.replace(/\/$/, '');
        this.secret = secret;
    }

    _signRequest(method, path) {
        const timestamp = Math.floor(Date.now() / 1000);
        const message = `${method}\n${path}\n${timestamp}`;
        const signature = crypto
            .createHmac('sha256', this.secret)
            .update(message)
            .digest('hex');
        return {
            'X-Timestamp': timestamp.toString(),
            'X-Signature': signature
        };
    }

    async fetchManifest() {
        const path = '/manifest';
        const headers = this._signRequest('GET', path);
        const url = new URL(path, this.baseURL);
        
        return new Promise((resolve, reject) => {
            const req = https.request(url, { headers }, (res) => {
                let data = '';
                res.on('data', chunk => data += chunk);
                res.on('end', () => {
                    if (res.statusCode === 200) {
                        resolve(JSON.parse(data));
                    } else {
                        reject(new Error(`HTTP ${res.statusCode}: ${data}`));
                    }
                });
            });
            req.on('error', reject);
            req.end();
        });
    }

    async downloadMod(modPath, saveTo) {
        const path = `/download/mods/${modPath}`;
        const headers = this._signRequest('GET', path);
        const url = new URL(path, this.baseURL);
        
        return new Promise((resolve, reject) => {
            const req = https.request(url, { headers }, (res) => {
                if (res.statusCode !== 200) {
                    reject(new Error(`HTTP ${res.statusCode}`));
                    return;
                }
                const file = fs.createWriteStream(saveTo);
                res.pipe(file);
                file.on('finish', () => {
                    file.close();
                    resolve();
                });
            });
            req.on('error', reject);
            req.end();
        });
    }
}

// Использование:
const client = new ShineCoreClient('https://api.be-sunshainy.ru', 'sun');
client.fetchManifest().then(manifest => {
    console.log(`Game version: ${manifest.dependencies.game_version}`);
});
```

### Go

```go
package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "net/http"
    "strconv"
    "time"
)

type ShineCoreClient struct {
    BaseURL string
    Secret  string
    Client  *http.Client
}

func (c *ShineCoreClient) signRequest(method, path string) (timestamp string, signature string) {
    ts := time.Now().Unix()
    message := method + "\n" + path + "\n" + strconv.FormatInt(ts, 64)
    mac := hmac.New(sha256.New, []byte(c.Secret))
    mac.Write([]byte(message))
    return strconv.FormatInt(ts, 10), hex.EncodeToString(mac.Sum(nil))
}

func (c *ShineCoreClient) FetchManifest() (*Manifest, error) {
    path := "/manifest"
    ts, sig := c.signRequest("GET", path)
    
    req, _ := http.NewRequest("GET", c.BaseURL+path, nil)
    req.Header.Set("X-Timestamp", ts)
    req.Header.Set("X-Signature", sig)
    
    resp, err := c.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var manifest Manifest
    return &manifest, json.NewDecoder(resp.Body).Decode(&manifest)
}

type Manifest struct {
    Project      string `json:"project"`
    Dependencies struct {
        GameVersion string   `json:"game_version"`
        JavaURLs    struct {
            Java8  string `json:"java_8"`
            Java17 string `json:"java_17"`
            Java21 string `json:"java_21"`
        } `json:"java_urls"`
    } `json:"dependencies"`
    Packages struct {
        Mods []struct {
            Path   string `json:"path"`
            Size   int64  `json:"size"`
            Sha256 string `json:"sha256"`
            URL    string `json:"url"`
        } `json:"mods"`
    } `json:"packages"`
}
```

## Важные моменты

### Безопасность

1. **Никогда не передавайте секрет в URL или query параметрах** — только в подписи
2. **Храните секрет безопасно** — не коммитьте в публичные репозитории
3. **Используйте HTTPS** — защищает от перехвата запросов

### Обработка ошибок

1. **401 Unauthorized** — проверьте секрет и синхронизацию времени
2. **404 Not Found** — файл не существует на сервере
3. **500 Internal Server Error** — временная проблема сервера, повторите запрос

### Временные окна

- Timestamp должен быть в пределах ±5 минут от текущего времени сервера
- Если время отличается — синхронизируйте системное время

### Параметры манифеста

- **`dependencies.game_version`** — версия Minecraft
- **`dependencies.loader`** — загрузчик (fabric/forge/neoforge)
- **`dependencies.java_urls`** — URL для загрузки Java 8/17/21 (zip архивы)
- **`packages.mods`** — список модов с путями, размерами и SHA256 хешами

### Мульти-лаунчер поддержка

Один сервер может обслуживать несколько лаунчеров:
- Используйте один секрет для всех клиентов
- Манифест содержит конфигурацию для всех лаунчеров
- Каждый лаунчер скачивает нужные моды по путям из манифеста

## Пример полного рабочего цикла

1. **Настройка клиента:**
   ```
   BaseURL = "https://api.be-sunshainy.ru"
   Secret = "sun"
   ```

2. **Получение манифеста:**
   - Создать подпись для `GET /manifest`
   - Отправить запрос с заголовками `X-Timestamp` и `X-Signature`
   - Получить JSON манифест

3. **Парсинг манифеста:**
   - Извлечь `game_version`, `loader`, `java_urls`
   - Получить список модов из `packages.mods`

4. **Загрузка файлов:**
   - Для каждого мода из `packages.mods`:
     - Создать подпись для `GET /download/mods/{path}`
     - Скачать файл по URL
     - Проверить SHA256 хеш файла

5. **Загрузка Java (опционально):**
   - Если нужна Java — используйте URL из `java_urls.java_8/17/21`
   - Это прямые ссылки на zip архивы (без авторизации)

## Конфигурация сервера

На сервере в `config.json`:
```json
{
  "secret": "sun",
  "base_dir": "./mods",
  ...
}
```


