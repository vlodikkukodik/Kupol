---
title: Dominio personale: collegarlo al sito
category: domains
description: Come puntare un dominio al sito e aggiungere sottodomini.
---
## Collegamento

1. Apri il sito → scheda «Domini» → inserisci il dominio (ad esempio `example.com`) → «Collega».
2. Il pannello mostra quale **record A** creare: nome del dominio → IP del server. Crealo presso il registrar o nella sezione [DNS](/dns), se il dominio è sui nostri name server.
3. Attendi l'aggiornamento del DNS (da pochi minuti a un giorno). Lo stato del dominio passerà da «in attesa del record A» a operativo e il certificato HTTPS verrà emesso automaticamente.

## Sottodomini e cartelle

All'interno del sito si possono creare sottodomini come `docs.example.com` e indicare per ciascuno una cartella.

## Domande frequenti

- **Dominio su un altro hosting.** Il record A si cambia presso chi gestisce il DNS del dominio: il registrar o un altro hosting.
- **Dominio con www.** Aggiungi `www` come dominio separato oppure un CNAME verso il dominio principale.
- **Il certificato non viene emesso.** Viene emesso solo quando il dominio punta già al server. Controlla il record A e attendi l'aggiornamento del DNS.

> Per la posta sul tuo dominio collega prima il dominio a un sito: la sezione «Posta» funziona solo con domini personali.
