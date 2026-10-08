---
title: FTP: caricare il sito con un programma
category: sites
description: Come attivare l'accesso FTP e collegarsi da FileZilla o da un altro programma.
---
## Attivazione

Apri il sito → scheda «FTP» → «Attiva FTP». Il pannello mostra server, porta, nome utente e password. **La password viene mostrata una sola volta**: salvala. Una password persa si può sostituire con una nuova; la vecchia smette di funzionare.

## Collegamento

- Protocollo: **FTPS** (FTP su TLS, «TLS esplicito»), modalità passiva.
- Server, porta e nome utente sono quelli della scheda «FTP» del sito. Il nome utente ha la forma `sito.utente`.
- Al primo collegamento il programma mostrerà il certificato del server: va accettato.

> Il semplice FTP senza cifratura invia password e file in chiaro, quindi conviene usare sempre FTPS.

## Account aggiuntivi

Per un sito si possono creare più account FTP con password diverse: ad esempio uno separato per un collaboratore. A ogni account si può assegnare una cartella, la modalità «sola lettura» e disattivarlo temporaneamente senza eliminarlo.

## Se non si collega

1. Controlla che siano selezionate la modalità passiva e la cifratura «TLS esplicito».
2. Verifica che FTP sia attivo nella scheda «FTP».
3. Dopo aver cambiato la password chiudi nel programma le vecchie connessioni.
4. Le reti domestiche a volte bloccano porte non standard: prova un'altra rete.
