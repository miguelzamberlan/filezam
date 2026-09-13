// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

//go:build !linux

package vfs

import "os"

func preallocate(f *os.File, size int64) error { return f.Truncate(size) }

func diskUsage(path string) DiskUsage { return DiskUsage{} }

func identity(fi os.FileInfo) (uint64, uint64) { return 0, 0 }
