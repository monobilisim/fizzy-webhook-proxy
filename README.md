# Fizzy Webhook Proxy

Fizzy'den gelen webhook isteklerini alıp Zulip, Google Chat ve Gotify gibi platformlara düzgün bir formatta ileten ara katman servisi.

Standart Fizzy bildirimleri karmaşık veya eksik olabiliyor. Bu servis araya girip mesajları temizliyor, başlıkları düzenliyor ve bozuk yorum linklerini onarıyor.

## Özellikler

- **Google Chat & Zulip:** Zengin kart görünümü (Başlık, Kişi, Detaylar ve Buton).
- **Akıllı Linkler:** Fizzy'nin bazen bozuk verdiği yorum linklerini düzeltir, tıklayınca direkt ilgili yoruma götürür.
- **Kolay Kurulum:** Tek komutla derlenip servise dönüşür.

## Kurulum

Sunucuda Go yüklü olması yeterli. Projeyi çektikten sonra:

```bash
sudo make install
```

Bu komut kodları derler ve `/usr/local/bin/fizzy-webhook-proxy` olarak sisteme kurar.

## Ayarlar

Servisin çalışması için `/etc/default/fizzy-webhook-proxy` dosyasını oluşturup gerekli webhook adreslerinin girilmesi gerek.

```bash
sudo vim /etc/default/fizzy-webhook-proxy
```

Örnek içerik:
```env
# Servis Portu
PORT=8080

# Kullanacağın servislerin webhook adresleri (Kullanılmayacak olan yazılmayabilir.)
ZULIP_WEBHOOK_URL=https://chat.example.com/api/v1/external/slack...
GOOGLE_CHAT_WEBHOOK_URL=https://chat.googleapis.com/v1/spaces/...
GOTIFY_WEBHOOK_URL=https://gotify.example.com/message?token=...

# Fizzy Link Düzeltme (Domain adresini yazman yeterli)
FIZZY_ROOT_URL=https://fizzy.example.com
```

## Servisi Başlatma

Ayarları yaptıktan sonra systemd servisini aktif et:

```bash
# Servis dosyasını kopyala
sudo cp deployment/fizzy-webhook-proxy.service /etc/systemd/system/

# Systemd'ye tanıt ve başlat
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

## Kullanım

Fizzy'de proje ayarlarına git, **Webhooks** kısmından yeni webhook ekle.
**Payload Format** olarak mutlaka **Generic JSON** seçmelisin.

URL kısmına proxy adresini yaz:
- Zulip için: `https://fizzy-webhook-proxy.example.com/zulip`
- Google Chat için: `https://fizzy-webhook-proxy.example.com/google-chat`
- Gotify için: `https://fizzy-webhook-proxy.example.com/gotify`
