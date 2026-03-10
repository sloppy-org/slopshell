# Design Lineage

Tabura's interaction model draws on a consistent thread in computing research: the workspace, not the app, should be the unit of design. This document traces that lineage and records the specific influences on Tabura's product decisions.

## Foundational papers

**Bush, "As We May Think" (1945)** — the Memex. A personal workspace where all documents are linked and annotable. The user follows association trails across materials, not app boundaries. Tabura's canvas-with-artifacts model is a direct descendant: receive a document, annotate it, link it to other artifacts, compose a response.

**Licklider, "Man-Computer Symbiosis" (1960)** — the computer as thinking partner, not discrete tool. Interaction is continuous, not transactional. This is the theoretical basis for Tabura's Dialogue mode: an ongoing collaborative presence rather than request-response prompting.

**Engelbart, "Augmenting Human Intellect: A Conceptual Framework" (1962)** — the theoretical paper behind NLS. The unit of design is the human activity system, not the software component. Engelbart's H-LAM/T framework (Human using Language, Artifacts, Methodology, in which he is Trained) argues that workspace, artifacts, and interaction grammar form one system. Tabura's five-noun ontology is a modern reduction of this.

**Nelson, Computer Lib / Dream Machines (1974)** — universal hypertext, transclusion, no app boundaries. Everything is one interconnected document space. Nelson's vision of documents that include and reference each other without copying is the conceptual ancestor of Tabura's artifact-reference model.

## Cognitive science foundations

**Gibson, The Ecological Approach to Visual Perception (1979)** — affordances in the original sense. The environment directly offers possibilities for action. A unified workspace affords working; app boundaries fragment the action space. Every context switch destroys ecological structure. This is the deepest theoretical argument for one surface.

**Suchman, Plans and Situated Actions (1987)** — human action is situated, not pre-planned. People respond to evolving context rather than executing pre-formed plans. This is a direct argument against immediate-dispatch (tap then AI fires) and for annotation-accumulation (observe, mark up, bundle, decide). Tabura's receive-annotate-bundle-send flow is Suchman-correct.

**Norman, The Design of Everyday Things (1988)** — mode errors. When the same gesture means different things in different modes, people make mistakes. Tabura's gesture truth table is the direct engineering response: no gesture may have two incompatible meanings in the same visible state.

**Hutchins, Cognition in the Wild (1995)** — cognition is distributed across people, artifacts, and spatial arrangements. The workspace is part of the cognitive system, not just a container for tools. An artifact arranged on a canvas is not decoration; it is thinking. Tabura's artifact-on-canvas model follows from this: spatial arrangement of artifacts is a cognitive act.

**Kirsh, "The Intelligent Use of Space" (1995)** — empirical study of how spatial arrangement simplifies choice, reduces search, and serves as external memory. All three functions break when the space is fragmented into app windows. This is empirical support for the single-canvas approach.

**Clark & Chalmers, "The Extended Mind" (1998)** — the notebook is part of the mind. If the workspace is an extension of cognition, fragmenting it across apps is fragmenting cognition. One unified surface is not a UX preference; it is a cognitive necessity.

**Polanyi, The Tacit Dimension (1966)** — subsidiary versus focal awareness. When using a tool well, you attend to the task, not the tool. The tool disappears into use. App switching forces focal awareness onto the tool. A unified surface allows tools to remain subsidiary.

## Interaction theory

**Winograd & Flores, Understanding Computers and Cognition (1986)** — brought Heidegger's readiness-to-hand into computing. Tools should be transparent in use. Every app boundary is a breakdown that forces the user to attend to the tool instead of the work.

**Kaptelinin & Nardi, Acting with Technology (2006)** — applied activity theory (Vygotsky, Leont'ev) to HCI. The unit of analysis is the activity, not the tool. The interface should be organized around the object of activity (what you are working on), not around which app you are in. This is the theoretical argument for organizing around Artifact and Item rather than application.

**Weiser, "The Computer for the 21st Century" (1991)** — calm technology. The most profound technologies disappear. Tabura's hidden chrome, edge-reveal panels, and indicator model follow from this: the system is present but not attention-demanding.

**Weiser & Brown, "The Coming Age of Calm Technology" (1996)** — center versus periphery of attention. Information should move smoothly between them. Items at the periphery, artifacts at the center, smooth transitions between. A notification that demands focus when it should be peripheral is a design failure.

**Raskin, The Humane Interface (2000)** — no modes, no apps, one continuous document surface. Raskin argued this was the only correct interaction model. Tabura's one calm surface principle is Raskin's thesis with modern capabilities.

## System precedents

**Xerox Star (1981)** — the first commercial GUI was document-centric, not app-centric. You opened documents; the system activated the right tools. When Apple and Microsoft copied the Star, they copied the window chrome but replaced the document-centric model with an app-centric one. Tabura returns to the Star's original intent.

**HyperCard (1987)** — stacks of cards, mixed media, embedded scripting. The stack was the unit of work and could be anything. It was the closest thing to one substrate for all creative work that millions of people actually used. Apple killed it by neglect because it undermined the app-as-product model.

**Canon Cat (Raskin, 1987)** — no desktop, no files, no apps, no modes. One continuous document that is everything. Navigate by searching. The most radical one-surface vision that shipped as a product.

**Oberon (Wirth, 1988)** — tiled text system where any text could be a command. One surface, no app/document distinction. The environment is the document is the shell.

**Emacs** — everything is a buffer. Mail, code, shell, calendar, notes, all in the same substrate. Tools vary; fundamental operations (point, mark, kill, yank) do not. This is the longest-running existence proof that tools vary, semantics don't works.

**Apple Newton (1993)** — pen-first, no files. The data model was a shared object store (soup), not a filesystem. Applications were views into the same data, not silos. Architecturally close to Tabura's single-ontology model.

**OpenDoc (1992-1997)** — compound documents where parts of a document could be handled by different editors. The document was sovereign. Failed because incumbent app vendors had no incentive to unbundle, and the component architecture was too complex for the hardware. Tabura avoids this failure mode: the AI layer replaces what OpenDoc tried to do with component architecture.

**Microsoft Courier (cancelled 2010)** — dual-screen journal. Pen-first, canvas plus journal, document-centric. The leaked designs show exactly the receive-annotate-bundle-send flow that Tabura's annotation model describes. Microsoft killed it because it threatened the Office business model.

**Smalltalk / Squeak** — one live object world. No files, no apps. Objects on a canvas, directly manipulable. Everything is inspectable and composable. Kay's Dynabook vision (1972): a personal dynamic medium, not a personal tool.

**Kay, "Personal Dynamic Media" (1977, with Goldberg)** — the Dynabook paper. Kay called it a medium, not a tool. A medium is something you think in. The app model turns the computer into a collection of tools; the medium model makes it a space for thought.

**Lisp Machines (1975-1990)** — one running image, everything introspectable, no app boundaries. The same principle as Smalltalk for the Lisp world.

## Modern research

**Ink & Switch, "Local-First Software" (2019, Kleppmann et al.)** — local data ownership, offline capability, collaboration without cloud dependency. Their subsequent Crosscut work (2023) addresses the data-trapped-in-app-silos problem directly. Tabura's filesystem-native approach sidesteps this: if everything is a real file in a real directory, there are no silos to bridge.

**Matuschak & Nielsen, "How can we develop transformative tools for thought?" (2019)** — tools for thought should be environments you inhabit, not apps you visit. Tool boundaries are artificial and cognitively harmful.

**Victor, "Inventing on Principle" (2012)** — creators need immediate connection to what they create. Indirection (menus, compile cycles, app switches) breaks the connection. A unified surface with direct manipulation preserves it.

**Olsen, "Evaluating User Interface Systems Research" (2007, CHI)** — HCI research over-values controlled micro-experiments and under-values systems contributions. The unified-environment vision is hard to validate with A/B tests on button placement, which partly explains why it keeps losing to incrementalist app design in academic publishing.

## The historical pattern

Every serious attempt to build a document-centric or environment-centric system has either been killed by platform owners because it threatened the app-as-purchase-unit business model (OpenDoc, Courier, HyperCard), survived only in niche contexts (Oberon, Smalltalk, Lisp Machines), been miscopied with the key insight stripped out (Star to Mac/Windows), or succeeded by being unfashionable enough to avoid corporate attention (Emacs).

The app as organizing unit is the aberration, not the norm, in computing's intellectual history. The dominant commercial model won for business reasons, not UX reasons.

## What Tabura takes from this

1. From the Star and Kay: the artifact, not the app, is the unit of work.
2. From Raskin and the Canon Cat: one surface, no modes, no hidden state. The gesture truth table is the engineering response to mode proliferation.
3. From Emacs: tools vary, fundamental operations do not. Seven action semantics, not a growing zoo.
4. From the Newton: shared data model, views not silos. Single ontology.
5. From Courier: pen-first annotation-accumulation-bundle-send is the right interaction flow.
6. From OpenDoc's failure: do not over-architect the component model. The AI layer replaces what component architectures tried to do.
7. From Suchman: action is situated. Accumulate context, then act. Do not immediately dispatch on every gesture.
8. From Hutchins and Clark: the workspace is cognition, not just a place where cognition happens.
9. From Weiser: the system should be calm. Present but not demanding.

Tabura's position (local-first, filesystem-native, no platform tax, AI as integration layer) avoids the historical failure modes. There is no app monopoly to protect, no component architecture to over-engineer, and the AI layer means tools do not need to be heavyweight app-like components.
