package pwhash

/*-
 * SPDX-License-Identifier: BSD-2-Clause-FreeBSD
 *
 * Copyright (c) 2011 The FreeBSD Project. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

/* Based on:
 * SHA256-based Unix crypt implementation. Released into the Public Domain by
 * Ulrich Drepper <drepper@redhat.com>. */
import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
)

type sha256crypt struct {
}

var (
	ErrNoSalt      = errors.New("no salt given form")
	ErrInvalidSalt = errors.New("invalid salt")
)
var (
	/* Define our magic string to mark salt for SHA256 "encryption" replacement. */
	sha256_salt_prefix = []byte("$5$")

	/* Prefix for optional rounds specification. */
	sha256_rounds_prefix = []byte("rounds=")
)

func (x *sha256crypt) crypt(pwd, slt []byte) []byte {
	// slt is [round=NUMBERS$]SALT
	return cryptSha256(pwd, slt)
}

func (x *sha256crypt) DetectHash(slt []byte) bool {
	return bytes.HasPrefix(slt, sha256_salt_prefix)
}

func cryptSha256(key, salt []byte) []byte {
	buffer := bytes.NewBuffer(nil)
	altResult := make([]byte, 32)
	tempResult := make([]byte, 32)
	ctx := sha256.New()
	altCtx := sha256.New()
	cnt := 0

	var cp, pBytes, sBytes []byte

	/* Default number of rounds. */
	rounds := ROUNDS_DEFAULT
	roundsCustom := false

	/* Find beginning of salt string. The prefix should normally always
	 * be present. Just in case it is not. */

	if bytes.HasPrefix(salt, sha256_salt_prefix) {
		salt = salt[3:]
	}

	if bytes.HasPrefix(salt, sha256_rounds_prefix) {
		num := salt[len(sha256_rounds_prefix):]
		index := bytes.IndexByte(num, byte('$')) // zakladajac, ze index >-0
		if index > 0 {
			var srounds, err = strconv.ParseUint(string(num[:index]), 10, 32)
			if err != nil {
				log.Fatalln("Rounds and squares", err)
			}
			salt = num[index+1:]
			rounds = int(math.Max(ROUNDS_MIN, math.Min(float64(srounds), ROUNDS_MAX)))
			roundsCustom = true
		}
	}
	// do we need this branching?
	index := bytes.IndexByte(salt, '$')
	if index < 0 {
		index = len(salt)
	}
	salt = salt[:int(math.Min(float64(index), SALT_LEN_MAX))]

	saltLen := len(salt)
	keyLen := len(key)

	/* Prepare for the real work. */
	ctx.Reset()

	/* Add the key string. */
	ctx.Write(key)

	/* The last part is the salt string. This must be at most 8
	 * characters and it ends at the first `$' character (for
	 * compatibility with existing implementations). */

	ctx.Write(salt)

	/* Compute alternate SHA256 sum with input KEY, SALT, and KEY. The
	 * final result will be added to the first context. */
	altCtx.Reset()

	/* Add key. */
	altCtx.Write(key)

	/* Add salt. */
	altCtx.Write(salt)

	/* Add key again. */
	altCtx.Write(key)

	/* Now get result of this (32 bytes) and add it to the other context. */
	altResult = altCtx.Sum(nil)

	/* Add for any character in the key one byte of the alternate sum. */
	for cnt = keyLen; cnt > 32; cnt -= 32 {
		ctx.Write(altResult)
	}
	ctx.Write(altResult[:cnt])

	/* Take the binary representation of the length of the key and for
	 * every 1 add the alternate sum, for every 0 the key. */
	for cnt = keyLen; cnt > 0; cnt >>= 1 {
		if (cnt & 1) != 0 {
			ctx.Write(altResult)
		} else {
			ctx.Write(key)
		}
	}
	/* Create intermediate result. */
	altResult = ctx.Sum(nil)

	/* Start computation of P byte sequence. */
	altCtx.Reset()

	/* For every character in the password add the entire password. */
	for cnt = 0; cnt < keyLen; cnt++ {
		altCtx.Write(key)
	}

	/* Finish the digest. */
	tempResult = altCtx.Sum(nil)

	/* Create byte sequence P. */
	cp = make([]byte, keyLen)
	pBytes = cp
	//cp = p_bytes = make(byte, key_len)
	for cnt = keyLen; cnt >= 32; cnt -= 32 {
		copy(cp[:32], tempResult[:32])
		cp = cp[32:]
	}
	copy(cp, tempResult[:cnt])

	/* Start computation of S byte sequence. */
	altCtx.Reset()

	/* For every character in the password add the entire password. */
	for cnt = 0; cnt < 16+int(altResult[0]); cnt++ {
		altCtx.Write(salt)
	}
	/* Finish the digest. */
	tempResult = altCtx.Sum(nil)

	/* Create byte sequence S. */
	cp = make([]byte, saltLen)
	sBytes = cp
	for cnt = saltLen; cnt >= 32; cnt -= 32 {
		copy(cp[:32], tempResult[:32])
		cp = cp[32:]
	}
	copy(cp, tempResult[:cnt])

	/* Repeatedly run the collected hash value through SHA256 to burn CPU
	 * cycles. */
	for cnt = 0; cnt < rounds; cnt++ {
		/* New context. */
		ctx.Reset()

		/* Add key or last result. */
		if (cnt & 1) != 0 {
			ctx.Write(pBytes[:keyLen])
		} else {
			ctx.Write(altResult[:32])
		}

		/* Add salt for numbers not divisible by 3. */
		if cnt%3 != 0 {
			ctx.Write(sBytes[:saltLen])
		}

		/* Add key for numbers not divisible by 7. */
		if cnt%7 != 0 {
			ctx.Write(pBytes[:keyLen])
		}

		/* Add key or last result. */
		if (cnt & 1) != 0 {
			ctx.Write(altResult[:32])
		} else {
			ctx.Write(pBytes[:keyLen])
		}

		/* Create intermediate result. */
		altResult = ctx.Sum(nil)
	}

	/* Now we can construct the result string. It consists of three
	 * parts. */
	buffer.Write(sha256_salt_prefix)
	if roundsCustom {
		buffer.WriteString(fmt.Sprintf("%s%d$", sha256_rounds_prefix, rounds))
	}
	buffer.Write(salt)
	buffer.WriteByte('$')

	b64From24bit(altResult[0], altResult[10], altResult[20], 4, buffer)
	b64From24bit(altResult[21], altResult[1], altResult[11], 4, buffer)
	b64From24bit(altResult[12], altResult[22], altResult[2], 4, buffer)
	b64From24bit(altResult[3], altResult[13], altResult[23], 4, buffer)
	b64From24bit(altResult[24], altResult[4], altResult[14], 4, buffer)
	b64From24bit(altResult[15], altResult[25], altResult[5], 4, buffer)
	b64From24bit(altResult[6], altResult[16], altResult[26], 4, buffer)
	b64From24bit(altResult[27], altResult[7], altResult[17], 4, buffer)
	b64From24bit(altResult[18], altResult[28], altResult[8], 4, buffer)
	b64From24bit(altResult[9], altResult[19], altResult[29], 4, buffer)
	b64From24bit(0, altResult[31], altResult[30], 3, buffer)

	//*cp = '\0';	/* Terminate the string. */

	/* Clear the buffer for the intermediate result so that people
	 * attaching to processes or reading core dumps cannot get any
	 * information. We do it in this way to clear correct_words[] inside
	 * the SHA256 implementation as well. */
	//SHA256_Init(&ctx);
	//SHA256_Final(alt_result, &ctx);
	//memset(temp_result, '\0', sizeof(temp_result));
	//memset(p_bytes, '\0', key_len);
	//memset(s_bytes, '\0', salt_len);
	ctx.Reset()
	altResult = ctx.Sum(nil)
	tempResult = nil
	pBytes = nil
	sBytes = nil

	return buffer.Bytes()
}
