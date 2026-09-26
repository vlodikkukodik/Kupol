---
title: Database
category: data
description: Creazione di PostgreSQL e MariaDB, accesso esterno e client web.
---
## Creazione

Apri la sezione [Database](/databases), scegli il DBMS (PostgreSQL o MariaDB), inserisci il nome — lettere latine minuscole e cifre — e premi «Crea il database». Il pannello crea il database e un account dedicato; **la password viene mostrata una sola volta**.

## Collegamento

- **Da un sito su questo server:** indirizzo e porta sono indicati nella scheda del database, nome utente e nome del database coincidono.
- **Dall'esterno:** aggiungi il tuo IP all'elenco degli indirizzi consentiti del database. Le connessioni esterne solo via TLS.
- **Nel browser:** il pulsante di accesso al client web apre Adminer direttamente sul database giusto senza digitare la password.

## Dimensione

Il database ha un limite di dimensione. Se viene superato, passa in sola lettura e arriva un'email; dopo aver ridotto i dati la scrittura si riattiva.

## Password

Una password persa si può sostituire nella scheda del database: la vecchia smette di funzionare e le applicazioni vanno riconfigurate.
