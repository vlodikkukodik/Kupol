---
title: Pianificatore di attività (cron)
category: apps
description: Attività programmate: richiesta a un indirizzo o comando nella cartella del sito.
---
## Cos'è

La sezione [Pianificatore](/cron) esegue attività secondo un orario: effettua una richiesta HTTP a un indirizzo oppure esegue un comando nella cartella del tuo sito. L'ora è UTC.

## Creare un'attività

1. Premi «Nuova attività», scegli il tipo (indirizzo o comando) e il sito.
2. Imposta la pianificazione (ad esempio «ogni 15 minuti» o una tua espressione cron).
3. Salva. Nel registro dell'attività si vede il risultato di ogni esecuzione: codice, output e durata.

## Limiti

Il numero di attività e l'intervallo minimo sono indicati nella pagina del pianificatore; per ogni esecuzione c'è un tempo limitato e nel registro restano le ultime esecuzioni. I comandi vengono eseguiti nella stessa sandbox del terminale del sito.

## Avvisi

Se un'attività termina con errore più volte di seguito, il pannello invia un'email (se non hai disattivato gli avvisi nelle [Impostazioni](/settings)).
