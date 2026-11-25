export function isImageType (contentType: string): boolean {
  return contentType.startsWith('image/')
}

export function isPdfType (contentType: string): boolean {
  return contentType === 'application/pdf'
}

export function formatSize (bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
