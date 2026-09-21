// Pagina «SU KUPOL»: presentazione, struttura, termini, regole, livelli, cronologia, informativa sulla riservatezza, sull'autore.
// Il canone dell'universo è in docs/organization.md; qui solo ciò che il canone conferma (senza le «linee nascoste»). Lo stesso testo va nel prerender.
export default {
  about: {
    title: 'SU KUPOL',
    kicker: "Presentazione e regole dell'archivio",
    intro:
      "KUPOL è l'archivio immaginario del Comitato per la Gestione degli Oggetti e dei Luoghi Paranormali: fascicoli di testo nello spirito dei dossier sovietici riservati. Tutto qui è opera di fantasia.",
    toc: 'Indice della pagina',
    tocLevels: 'Livelli di accesso',
    tocTimeline: 'Cronologia',
    tocPrivacy: 'Riservatezza',
    tocAuthor: "Sull'autore",

    sections: {
      about: {
        title: 'Presentazione',
        p1: "Il Comitato per la Gestione degli Oggetti e dei Luoghi Paranormali (KUPOL) è stato fondato nel 1974 e dipende direttamente dal Consiglio dei ministri dell'URSS. È un unico istituto chiuso con sedi distaccate — gli «oggetti numerati»; il quartier generale è l'Oggetto «Kupol-1», vicino a Mosca. Lo dirige un anonimo «Capo di KUPOL». Il motto del Comitato: «Sotto la Cupola — silenzio».",
        p2: "Il Comitato opera solo sul territorio dell'URSS; dopo il 1991 continua a esistere sotto la Federazione Russa con lo stesso nome. Ufficialmente KUPOL non esiste — è una negazione totale e coerente.",
        p3: "Il timbro di riservatezza dei documenti è «Modulo KUPOL-1», lo statuto è il «Regolamento della Cupola». I fascicoli sono conservati nell'Archivio centrale di Kupol (ACK) e gli oggetti sono censiti nel Registro degli Oggetti di Kupol (ROK).",
      },
      structure: {
        title: 'Struttura e reparti',
        p1: "Gradi dei dipendenti: Dipendente → Dipendente anziano → Sorvegliante → Curatore (gradi I–IV, il quarto è il più alto). Sopra di loro c'è il Consiglio Speciale di KUPOL, composto da tre–cinque Curatori di IV grado.",
        p2: 'Il lavoro del Comitato è suddiviso tra i reparti:',
        i1: "Reparto sequestri — squadre operative di cattura; la procedura dipende dalla classe dell'oggetto.",
        i2: 'Reparto contenimento — sorveglianza e protocolli di contenimento.',
        i3: 'Reparto studi scientifici — esperimenti.',
        i4: 'Reparto sicurezza interna — controspionaggio e lotta alle fughe di notizie.',
        i5: 'Gruppi speciali armati — con numeri e nominativi.',
      },
      terms: {
        title: 'Termini',
        p1: 'Alcune parole senza le quali è difficile leggere i dossier:',
        i1: "Oggetto (O-N.) — creatura, fenomeno o luogo anomalo messo a registro; le classi di pericolo vanno da 1 a 5.",
        i2: 'Fenomeno — evento anomalo; il suo scostamento dalla norma si misura in punti di deviazione (p.d.).',
        i3: 'SK-7, in gergo «Candela» — lo strumento con cui si misura questo scostamento.',
        i4: "Fonte primaria (Oggetto O-0) — oggetto «zero» fuori dalla numerazione generale, fonte comune delle anomalie; la sua esistenza nell'organizzazione non è un segreto, ma l'accesso diretto ce l'hanno solo i Curatori di IV grado.",
      },
      rules: {
        title: "Come leggere l'archivio",
        p1: "Ogni documento, ogni suo blocco e persino ogni singolo frammento di testo ha un livello di accesso. Il lettore vede solo ciò che non supera il proprio accesso; tutto il resto il server non lo invia affatto — sulla pagina resta solo una striscia nera «Dati eliminati».",
        p2: "Un documento superiore al tuo accesso si comporta secondo la decisione dell'autore: o «Accesso negato» con l'indicazione del livello necessario, oppure «Fascicolo non trovato» — come se quel documento non esistesse. Le bozze e i documenti in verifica li vedono solo i dipendenti.",
        p3: "Le sigle (sui documenti sono in cirillico, qui in trascrizione latina): «O-041» — Oggetto, «PRIKAZ-1978-12» — ordine, «INC-1982-07» — incidente, «LD-0157» — fascicolo personale, «OTD-2» e «OB-14» — reparto e sede, «PROT-1979-03» — verbale, «POK-1980-22» — testimonianze, «MEMO-5» — memorandum. La sigla si può digitare con qualsiasi tastiera e maiuscole o minuscole: «o-41», «O-041».",
      },
    },

    levels: {
      title: 'Livelli di accesso',
      lead: 'Il livello determina quali documenti, blocchi e frammenti vedi. È indicato nel tuo fascicolo personale e sul tesserino nell’intestazione del sito.',
      region: 'Livelli di accesso',
      caption: 'Livelli di accesso e come ottenerli',
      colLevel: 'Livello',
      colRank: 'Grado',
      colHow: 'Come ottenerlo',
      how: {
        0: 'senza registrazione: solo documenti aperti',
        1: 'assegnato con la registrazione',
        2: "per la partecipazione alla vita dell'archivio",
        3: "per la partecipazione alla vita dell'archivio",
        4: 'lo assegna il Consiglio Speciale',
        5: 'lo assegna il Consiglio Speciale',
        6: 'lo assegna il Consiglio Speciale',
        7: "solo l'autore dell'archivio; vede tutto, comprese le bozze",
      },
    },

    timeline: {
      title: 'Cronologia',
      lead: 'Eventi dal 1974 a oggi. Sono mostrati quelli accessibili con il tuo livello; una parte degli eventi può essere oscurata.',
      loading: 'Caricamento della cronologia…',
      empty: 'Nella cronologia non ci sono ancora eventi accessibili a te.',
      access: 'accesso {level}',
    },

    privacy: {
      title: 'Informativa sulla riservatezza',
      intro: "L'archivio conserva del lettore il minimo necessario al funzionamento. Sul sito non ci sono pubblicità, analisi né contatori di terze parti.",
      i1: {
        term: 'Login e password',
        text: 'Il login è visibile nel tuo fascicolo personale e nella firma dei documenti che hai redatto. La password non è conservata in chiaro da nessuna parte — solo il suo hash irreversibile (argon2id); lo stesso vale per il codice di riserva.',
      },
      i2: {
        term: 'Sessioni e cookie',
        text: "Dopo l'accesso il sito imposta un solo cookie tecnico di sessione (kupol_session): serve a mantenerti connesso e non serve a sorvegliarti. Senza di esso l'accesso è impossibile, perciò non occorre il tuo consenso. Altri cookie non ce ne sono.",
      },
      i3: {
        term: 'Indirizzo IP e browser',
        text: "Con la sessione si conservano l'indirizzo IP e il nome del browser — per proteggere l'account e analizzare i guasti; l'indirizzo IP finisce anche nei registri tecnici del server.",
      },
      i4: {
        term: 'Cronologia delle letture',
        text: "L'archivio ricorda quali documenti pubblicati hai aperto (per il fascicolo personale e per future statistiche). Questa cronologia non viene mostrata agli altri lettori.",
      },
      i5: { term: "Codice dall'app", text: "Se attivi l'accesso con il codice dell'app, il relativo segreto è conservato sul server in forma cifrata." },
      i6: {
        term: 'Eliminazione',
        text: 'Il pulsante «Consegna il fascicolo» nel fascicolo personale elimina l’account e i dati collegati completamente e senza possibilità di recupero.',
      },
    },

    author: {
      title: "Sull'autore e contatti",
      text: "L'archivio è scritto e curato da un solo autore. Errori, domande sul funzionamento del sito e proposte sul canone vanno inviati ai contatti qui sotto.",
    },
    pageTitle: "SU KUPOL — l'archivio immaginario KUPOL",
  },
}
