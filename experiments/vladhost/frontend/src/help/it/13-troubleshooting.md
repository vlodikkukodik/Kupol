---
title: Cosa fare se il sito non si apre
category: start
description: Un breve elenco di controlli e dove scrivere se non aiuta.
---
## Controlla in ordine

1. **Indirizzo.** Apri l'indirizzo dalla «Panoramica» del sito, non a memoria: il nome è formato dal sito e dal tuo nome utente.
2. **File.** Nella scheda «File», nella radice deve esserci `index.html` (o `index.php` per PHP). Se lo ZIP si è estratto in una sottocartella, sposta i file un livello più su.
3. **HTTPS.** Scheda «SSL»: finché il certificato è «in emissione», il browser può segnalare problemi di connessione. Attendi un minuto.
4. **Dominio personale.** Scheda «Domini»: lo stato «in attesa del record A» significa che il DNS non punta ancora al server. Controlla il record e attendi l'aggiornamento del DNS (fino a un giorno).
5. **Ambiente.** Per PHP e applicazioni apri le schede «Ambiente» e «Registri»: l'errore di solito è scritto lì.
6. **Cache del browser.** Apri il sito in una finestra privata o ricarica la pagina svuotando la cache.
7. **Disco.** Se i file non si caricano, controlla lo spazio occupato nella «Panoramica»: la quota è di 500 MB per account.

## Dove guardare

- «Registri» del sito: accessi ed errori, con ricerca e download.
- «Statistiche»: si vede se arrivano richieste al sito e quali codici di risposta vengono restituiti.

## Non è servito?

Scrivici nel [Supporto](/support): indica l'indirizzo del sito, cosa hai fatto e cosa vedi. Uno screenshot dell'errore e l'ora in cui è comparso accelerano la risposta.
