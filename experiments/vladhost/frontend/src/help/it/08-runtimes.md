---
title: PHP, Node.js e Python
category: apps
description: Come attivare l'ambiente di esecuzione del sito e leggere il registro dell'applicazione.
---
## Scelta dell'ambiente

Apri il sito → scheda «Ambiente» e scegli:

- **Statico** — i file vengono serviti così come sono.
- **PHP** — gli script `.php` vengono eseguiti in un pool dedicato al sito, la versione si può scegliere. `.htaccess` funziona (ad esempio il reindirizzamento a `index.php`).
- **Node.js** e **Python** — la tua applicazione viene avviata come servizio. Il pannello passa la porta nella variabile `PORT`; bisogna ascoltare l'indirizzo `127.0.0.1`. Tutte le richieste al sito vanno all'applicazione.

## Comando di avvio (Node.js e Python)

Indica una riga di comando, ad esempio:

```
node server.js
```

Il comando viene eseguito nella cartella del sito. Dopo aver modificato il codice premi «Riavvia».

## Registro e stato

Nella scheda «Ambiente» si vedono lo stato dell'applicazione e le ultime righe del registro. Gli errori PHP vengono scritti nel registro del sito.

## Sicurezza

Ogni sito funziona con un utente di sistema dedicato e vede solo la propria cartella. Le reti interne non sono raggiungibili e l'esecuzione di comandi di sistema da PHP è disattivata.
