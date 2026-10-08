---
title: HTTPS e certificati
category: domains
description: Come vengono emessi i certificati, cosa significano gli stati e come riemetterli.
---
## Come funziona

Per l'indirizzo del sito, i suoi sottodomini e i domini personali collegati il pannello emette da solo certificati gratuiti Let's Encrypt e li rinnova. Non serve fare nulla.

## Stati

- **HTTPS funziona** — il certificato è emesso, è indicata la scadenza.
- **In emissione** — l'emissione è in corso, di solito meno di un minuto.
- **In attesa del record A** — il dominio personale non punta ancora al server (vedi «Dominio personale»).
- **Errore** — l'emissione non è riuscita; il motivo è indicato accanto e arriva anche una email.

## Riemissione

Nella scheda «SSL» del sito c'è il pulsante di riemissione. Serve se hai cambiato il DNS o se l'emissione precedente è finita con un errore.

## Email sui certificati

Due settimane prima della scadenza e ancora tre giorni prima il pannello ti scrive se il rinnovo automatico non ha funzionato. Le email arrivano all'indirizzo confermato e si possono disattivare nelle [Impostazioni](/settings).

> Let's Encrypt ha dei limiti sul numero di certificati. Se l'emissione viene rifiutata per il limite, riprova più tardi.
