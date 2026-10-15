/** Enfocar al abrir solo con ratón/trackpad: en el móvil abriría el teclado y taparía la pantalla. */
export const finePointer = () => typeof window !== 'undefined' && !!window.matchMedia?.('(pointer: fine)').matches
