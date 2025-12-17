# Fizzy Webhook Proxy

Fizzy'den gelen webhook isteklerini alıp Zulip, Google Chat ve Gotify gibi platformlara düzgün bir formatta ileten ara katman servisi.

Standart Fizzy bildirimleri karmaşık veya eksik olabiliyor. Bu servis araya girip mesajları temizliyor, başlıkları düzenliyor ve bozuk yorum linklerini onarıyor.

## Özellikler

- **Zengin Bildirimler:** Google Chat ve Zulip için kart görünümleri, Gotify için düzenli Markdown formatı.
- **Akıllı Linkler:** Yorum linklerini düzeltir, ilgili karta ve yorum ID'sine yönlendirir.
- **Deduplication:** Aynı olayın yanlışlıkla birden fazla kez bildirilmesini engeller.
- **Kolay Kurulum:** Tek bir binary dosya olarak çalışır.

## Kurulum (Binary ile)

En son sürümü [GitHub Releases](https://github.com/monobilisim/fizzy-webhook-proxy/releases) sayfasından indirebilirsiniz.

```bash
# Binary'yi indir ve çalıştırılabilir yap
wget https://github.com/monobilisim/fizzy-webhook-proxy/releases/latest/download/fizzy-webhook-proxy
chmod +x fizzy-webhook-proxy
sudo mv fizzy-webhook-proxy /usr/local/bin/
```

Alternatif olarak kaynak kodu derlemek isterseniz:
```bash
sudo make install
```

## Ayarlar

Servisin çalışması için `/etc/default/fizzy-webhook-proxy` dosyasında (veya `.env` dosyasında) ayarların yapılması gerekir:

```bash
sudo vim /etc/default/fizzy-webhook-proxy
```

```env
# Servis Portu
PORT=8080

# Webhook Adresleri (Kullanılmayanları boş bırakabilirsiniz)
ZULIP_WEBHOOK_URL=https://chat.example.com/api/v1/external/slack...
GOOGLE_CHAT_WEBHOOK_URL=https://chat.googleapis.com/v1/spaces/...
GOTIFY_WEBHOOK_URL=https://gotify.example.com/message?token=...

# Fizzy Link Düzeltme
FIZZY_ROOT_URL=https://fizzy.example.com
```

## Servisi Başlatma

```bash
sudo cp deployment/fizzy-webhook-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

## Bilinen Limitasyonlar

Fizzy Webhook altyapısındaki bazı veri eksiklikleri nedeniyle:

1.  **Yorum Bildirimlerinde Kart Başlığı:**
    - Fizzy, `comment_created` olayında kartın metin başlığını (örn. "Login Sayfası Hatası") göndermez.
    - **Çözüm:** Proxy, URL'den kart numarasını ayıklar ve başlık eksikse `[Kişi] yorum yaptı` şeklinde sade bir başlık gösterir. Metin başlığına API entegrasyonu olmadan erişilemez.

2.  **Atama Bildirimlerinde Kişi İsmi:**
    - `card_assigned` olayında kartın *kime* atandığı bilgisi payload içinde gelmez.
    - **Çözüm:** Bildirim "kartı birine atadı" şeklinde genel bir ifadeyle gösterilir.

3.  **Çifte Bildirimler:**
    - Fizzy bazen aynı olayı (özellikle `card_reopened`) milisaniyeler içinde iki kez gönderebilir.
    - **Çözüm:** Proxy içinde 2 saniyelik bir `deduplication` mekanizması vardır; aynı olay tekrarlanırsa ikincisi yoksayılır.

## Kullanım

Fizzy proje ayarlarından **Webhooks** ekleyin. Aşağıdaki olayları (Events) seçmenizi öneririz:

- `card_created`, `card_published`
- `comment_created`
- `card_moved`, `card_board_changed`
- `card_assigned`, `card_unassigned`
- `card_closed`, `card_reopened`, `card_archived`
- `card_postponed`, `card_sent_back_to_triage`

URL kısmına proxy adresini yaz:
- Zulip için: `https://proxy.adresiniz.com/zulip`
- Google Chat için: `https://proxy.adresiniz.com/google-chat`
- Gotify için: `https://proxy.adresiniz.com/gotify`
