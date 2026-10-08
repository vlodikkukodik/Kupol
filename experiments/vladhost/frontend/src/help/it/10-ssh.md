---
title: SSH e terminale
category: access
description: Accesso con chiave, terminale nel browser, scp, rsync e git.
---
## Chiavi

Nella sezione [Chiavi SSH](/ssh) aggiungi una chiave pubblica (una riga da `id_ed25519.pub`) oppure chiedi al pannello di creare una coppia: **la parte privata viene mostrata una sola volta** e non viene conservata. Fino a dieci chiavi per account.

## Attivazione dell'accesso

Apri il sito → scheda «Terminale» → attiva «Accesso alla shell». Il sito riceve un utente di sistema dedicato.

## Collegamento

```
ssh sito.utente@ssh.vladinc.ru -p 2222
```

Le password non sono accettate, solo le chiavi. Il comando esatto e l'impronta della chiave del server sono mostrati nella scheda «Terminale»: al primo collegamento confronta l'impronta.

Funzionano anche `scp`, `rsync`, `sftp` e `git` via SSH: stesso nome utente e stessa chiave.

## Terminale nel browser

Il pulsante «Apri il terminale» apre la shell direttamente nella pagina, senza chiave.

## Limiti della sandbox

La shell vede la cartella del sito e la propria cartella temporanea. Memoria 512 MB, fino a 128 processi, sessione al massimo 8 ore, 30 minuti di inattività chiudono la connessione. L'accesso alle reti interne è chiuso. La home è temporanea: non conservarvi chiavi o token.
