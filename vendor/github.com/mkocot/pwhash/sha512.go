package pwhash

import (
	"bytes"
	"crypto/sha512"
	"fmt"
	"log"
	"math"
	"strconv"
)

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
 * SHA512-based Unix crypt implementation. Released into the Public Domain by
 * Ulrich Drepper <drepper@redhat.com>. */

var (

	/* Define our magic string to mark salt for SHA512 "encryption" replacement. */
	sha512_salt_prefix = []byte("$6$")

	/* Prefix for optional rounds specification. */
	sha512_rounds_prefix = []byte("rounds=")
)

type sha512crypt struct{}

func (x *sha512crypt) crypt(pwd, slt []byte) []byte {
	// slt is [round=NUMBERS$]SALT
	return crypt_sha512(pwd, slt)
}

func (x *sha512crypt) DetectHash(slt []byte) bool {
	return bytes.HasPrefix(slt, sha256_salt_prefix)
}

func crypt_sha512(key, salt []byte) []byte {
	buffer := bytes.NewBuffer(nil)
	altResult := make([]byte, 32)
	tempResult := make([]byte, 32)
	ctx := sha512.New()
	altCtx := sha512.New()
	cnt := 0

	var cp, pBytes, sBytes []byte

	/* Default number of rounds. */
	rounds := ROUNDS_DEFAULT
	roundsCustom := false

	/* Find beginning of salt string. The prefix should normally always
	 * be present. Just in case it is not. */

	if bytes.HasPrefix(salt, sha512_salt_prefix) {
		salt = salt[3:]
	}

	if bytes.HasPrefix(salt, sha512_rounds_prefix) {
		num := salt[len(sha512_rounds_prefix):]
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
	for cnt = keyLen; cnt > 64; cnt -= 64 {
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
	/* Create byte sequence P. */
	cp = make([]byte, keyLen)
	pBytes = cp
	//cp = p_bytes = make(byte, key_len)
	for cnt = keyLen; cnt >= 64; cnt -= 64 {
		copy(cp[:64], tempResult[:64])
		cp = cp[64:]
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
	for cnt = saltLen; cnt >= 64; cnt -= 64 {
		copy(cp[:64], tempResult[:64])
		cp = cp[64:]
	}
	copy(cp, tempResult[:cnt])

	/* Repeatedly run the collected hash value through SHA512 to burn CPU
	 * cycles. */
	for cnt = 0; cnt < rounds; cnt++ {
		/* New context. */
		ctx.Reset()

		/* Add key or last result. */
		if (cnt & 1) != 0 {
			ctx.Write(pBytes[:keyLen])
		} else {
			ctx.Write(altResult)
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
			ctx.Write(altResult)
		} else {
			ctx.Write(pBytes[:keyLen])
		}

		/* Create intermediate result. */
		altResult = ctx.Sum(nil)
	}

	/* Now we can construct the result string. It consists of three
	 * parts. */
	buffer.Write(sha512_salt_prefix)
	if roundsCustom {
		buffer.WriteString(fmt.Sprintf("%s%d$", sha512_rounds_prefix, rounds))
	}
	buffer.Write(salt)
	buffer.WriteByte('$')

	b64From24bit(altResult[0], altResult[21], altResult[42], 4, buffer)
	b64From24bit(altResult[22], altResult[43], altResult[1], 4, buffer)
	b64From24bit(altResult[44], altResult[2], altResult[23], 4, buffer)
	b64From24bit(altResult[3], altResult[24], altResult[45], 4, buffer)
	b64From24bit(altResult[25], altResult[46], altResult[4], 4, buffer)
	b64From24bit(altResult[47], altResult[5], altResult[26], 4, buffer)
	b64From24bit(altResult[6], altResult[27], altResult[48], 4, buffer)
	b64From24bit(altResult[28], altResult[49], altResult[7], 4, buffer)
	b64From24bit(altResult[50], altResult[8], altResult[29], 4, buffer)
	b64From24bit(altResult[9], altResult[30], altResult[51], 4, buffer)
	b64From24bit(altResult[31], altResult[52], altResult[10], 4, buffer)
	b64From24bit(altResult[53], altResult[11], altResult[32], 4, buffer)
	b64From24bit(altResult[12], altResult[33], altResult[54], 4, buffer)
	b64From24bit(altResult[34], altResult[55], altResult[13], 4, buffer)
	b64From24bit(altResult[56], altResult[14], altResult[35], 4, buffer)
	b64From24bit(altResult[15], altResult[36], altResult[57], 4, buffer)
	b64From24bit(altResult[37], altResult[58], altResult[16], 4, buffer)
	b64From24bit(altResult[59], altResult[17], altResult[38], 4, buffer)
	b64From24bit(altResult[18], altResult[39], altResult[60], 4, buffer)
	b64From24bit(altResult[40], altResult[61], altResult[19], 4, buffer)
	b64From24bit(altResult[62], altResult[20], altResult[41], 4, buffer)
	b64From24bit(0, 0, altResult[63], 2, buffer)

	// *cp = '\0';	/* Terminate the string. */

	/* Clear the buffer for the intermediate result so that people
	 * attaching to processes or reading core dumps cannot get any
	 * information. We do it in this way to clear correct_words[] inside
	 * the SHA512 implementation as well. */
	//  SHA512_Init(&ctx);
	//  SHA512_Final(altResult, &ctx);
	//  memset(temp_result, '\0', sizeof(temp_result));
	//  memset(p_bytes, '\0', key_len);
	//  memset(s_bytes, '\0', salt_len);

	ctx.Reset()
	altResult = ctx.Sum(nil)
	tempResult = nil
	pBytes = nil
	sBytes = nil

	return buffer.Bytes()
}

//  #ifdef TEST

//  static const struct {
// 	 const char *input;
// 	 const char result[64];
//  } tests[] =
//  {
// 	 /* Test vectors from FIPS 180-2: appendix C.1. */
// 	 {
// 		 "abc",
// 		 "\xdd\xaf\x35\xa1\x93\x61\x7a\xba\xcc\x41\x73\x49\xae\x20\x41\x31"
// 		 "\x12\xe6\xfa\x4e\x89\xa9\x7e\xa2\x0a\x9e\xee\xe6\x4b\x55\xd3\x9a"
// 		 "\x21\x92\x99\x2a\x27\x4f\xc1\xa8\x36\xba\x3c\x23\xa3\xfe\xeb\xbd"
// 		 "\x45\x4d\x44\x23\x64\x3c\xe8\x0e\x2a\x9a\xc9\x4f\xa5\x4c\xa4\x9f"
// 	 },
// 	 /* Test vectors from FIPS 180-2: appendix C.2. */
// 	 {
// 		 "abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmn"
// 		 "hijklmnoijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu",
// 		 "\x8e\x95\x9b\x75\xda\xe3\x13\xda\x8c\xf4\xf7\x28\x14\xfc\x14\x3f"
// 		 "\x8f\x77\x79\xc6\xeb\x9f\x7f\xa1\x72\x99\xae\xad\xb6\x88\x90\x18"
// 		 "\x50\x1d\x28\x9e\x49\x00\xf7\xe4\x33\x1b\x99\xde\xc4\xb5\x43\x3a"
// 		 "\xc7\xd3\x29\xee\xb6\xdd\x26\x54\x5e\x96\xe5\x5b\x87\x4b\xe9\x09"
// 	 },
// 	 /* Test vectors from the NESSIE project. */
// 	 {
// 		 "",
// 		 "\xcf\x83\xe1\x35\x7e\xef\xb8\xbd\xf1\x54\x28\x50\xd6\x6d\x80\x07"
// 		 "\xd6\x20\xe4\x05\x0b\x57\x15\xdc\x83\xf4\xa9\x21\xd3\x6c\xe9\xce"
// 		 "\x47\xd0\xd1\x3c\x5d\x85\xf2\xb0\xff\x83\x18\xd2\x87\x7e\xec\x2f"
// 		 "\x63\xb9\x31\xbd\x47\x41\x7a\x81\xa5\x38\x32\x7a\xf9\x27\xda\x3e"
// 	 },
// 	 {
// 		 "a",
// 		 "\x1f\x40\xfc\x92\xda\x24\x16\x94\x75\x09\x79\xee\x6c\xf5\x82\xf2"
// 		 "\xd5\xd7\xd2\x8e\x18\x33\x5d\xe0\x5a\xbc\x54\xd0\x56\x0e\x0f\x53"
// 		 "\x02\x86\x0c\x65\x2b\xf0\x8d\x56\x02\x52\xaa\x5e\x74\x21\x05\x46"
// 		 "\xf3\x69\xfb\xbb\xce\x8c\x12\xcf\xc7\x95\x7b\x26\x52\xfe\x9a\x75"
// 	 },
// 	 {
// 		 "message digest",
// 		 "\x10\x7d\xbf\x38\x9d\x9e\x9f\x71\xa3\xa9\x5f\x6c\x05\x5b\x92\x51"
// 		 "\xbc\x52\x68\xc2\xbe\x16\xd6\xc1\x34\x92\xea\x45\xb0\x19\x9f\x33"
// 		 "\x09\xe1\x64\x55\xab\x1e\x96\x11\x8e\x8a\x90\x5d\x55\x97\xb7\x20"
// 		 "\x38\xdd\xb3\x72\xa8\x98\x26\x04\x6d\xe6\x66\x87\xbb\x42\x0e\x7c"
// 	 },
// 	 {
// 		 "abcdefghijklmnopqrstuvwxyz",
// 		 "\x4d\xbf\xf8\x6c\xc2\xca\x1b\xae\x1e\x16\x46\x8a\x05\xcb\x98\x81"
// 		 "\xc9\x7f\x17\x53\xbc\xe3\x61\x90\x34\x89\x8f\xaa\x1a\xab\xe4\x29"
// 		 "\x95\x5a\x1b\xf8\xec\x48\x3d\x74\x21\xfe\x3c\x16\x46\x61\x3a\x59"
// 		 "\xed\x54\x41\xfb\x0f\x32\x13\x89\xf7\x7f\x48\xa8\x79\xc7\xb1\xf1"
// 	 },
// 	 {
// 		 "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq",
// 		 "\x20\x4a\x8f\xc6\xdd\xa8\x2f\x0a\x0c\xed\x7b\xeb\x8e\x08\xa4\x16"
// 		 "\x57\xc1\x6e\xf4\x68\xb2\x28\xa8\x27\x9b\xe3\x31\xa7\x03\xc3\x35"
// 		 "\x96\xfd\x15\xc1\x3b\x1b\x07\xf9\xaa\x1d\x3b\xea\x57\x78\x9c\xa0"
// 		 "\x31\xad\x85\xc7\xa7\x1d\xd7\x03\x54\xec\x63\x12\x38\xca\x34\x45"
// 	 },
// 	 {
// 		 "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789",
// 		 "\x1e\x07\xbe\x23\xc2\x6a\x86\xea\x37\xea\x81\x0c\x8e\xc7\x80\x93"
// 		 "\x52\x51\x5a\x97\x0e\x92\x53\xc2\x6f\x53\x6c\xfc\x7a\x99\x96\xc4"
// 		 "\x5c\x83\x70\x58\x3e\x0a\x78\xfa\x4a\x90\x04\x1d\x71\xa4\xce\xab"
// 		 "\x74\x23\xf1\x9c\x71\xb9\xd5\xa3\xe0\x12\x49\xf0\xbe\xbd\x58\x94"
// 	 },
// 	 {
// 		 "123456789012345678901234567890123456789012345678901234567890"
// 		 "12345678901234567890",
// 		 "\x72\xec\x1e\xf1\x12\x4a\x45\xb0\x47\xe8\xb7\xc7\x5a\x93\x21\x95"
// 		 "\x13\x5b\xb6\x1d\xe2\x4e\xc0\xd1\x91\x40\x42\x24\x6e\x0a\xec\x3a"
// 		 "\x23\x54\xe0\x93\xd7\x6f\x30\x48\xb4\x56\x76\x43\x46\x90\x0c\xb1"
// 		 "\x30\xd2\xa4\xfd\x5d\xd1\x6a\xbb\x5e\x30\xbc\xb8\x50\xde\xe8\x43"
// 	 }
//  };

//  #define ntests (sizeof (tests) / sizeof (tests[0]))

//  static const struct {
// 	 const char *salt;
// 	 const char *input;
// 	 const char *expected;
//  } tests2[] =
//  {
// 	 {
// 		 "$6$saltstring", "Hello world!",
// 		 "$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJu"
// 		 "esI68u4OTLiBFdcbYEdFCoEOfaS35inz1"
// 	 },
// 	 {
// 		 "$6$rounds=10000$saltstringsaltstring", "Hello world!",
// 		 "$6$rounds=10000$saltstringsaltst$OW1/O6BYHV6BcXZu8QVeXbDWra3Oeqh0sb"
// 		 "HbbMCVNSnCM/UrjmM0Dp8vOuZeHBy/YTBmSK6H9qs/y3RnOaw5v."
// 	 },
// 	 {
// 		 "$6$rounds=5000$toolongsaltstring", "This is just a test",
// 		 "$6$rounds=5000$toolongsaltstrin$lQ8jolhgVRVhY4b5pZKaysCLi0QBxGoNeKQ"
// 		 "zQ3glMhwllF7oGDZxUhx1yxdYcz/e1JSbq3y6JMxxl8audkUEm0"
// 	 },
// 	 {
// 		 "$6$rounds=1400$anotherlongsaltstring",
// 		 "a very much longer text to encrypt.  This one even stretches over more"
// 		 "than one line.",
// 		 "$6$rounds=1400$anotherlongsalts$POfYwTEok97VWcjxIiSOjiykti.o/pQs.wP"
// 		 "vMxQ6Fm7I6IoYN3CmLs66x9t0oSwbtEW7o7UmJEiDwGqd8p4ur1"
// 	 },
// 	 {
// 		 "$6$rounds=77777$short",
// 		 "we have a short salt string but not a short password",
// 		 "$6$rounds=77777$short$WuQyW2YR.hBNpjjRhpYD/ifIw05xdfeEyQoMxIXbkvr0g"
// 		 "ge1a1x3yRULJ5CCaUeOxFmtlcGZelFl5CxtgfiAc0"
// 	 },
// 	 {
// 		 "$6$rounds=123456$asaltof16chars..", "a short string",
// 		 "$6$rounds=123456$asaltof16chars..$BtCwjqMJGx5hrJhZywWvt0RLE8uZ4oPwc"
// 		 "elCjmw2kSYu.Ec6ycULevoBK25fs2xXgMNrCzIMVcgEJAstJeonj1"
// 	 },
// 	 {
// 		 "$6$rounds=10$roundstoolow", "the minimum number is still observed",
// 		 "$6$rounds=1000$roundstoolow$kUMsbe306n21p9R.FRkW3IGn.S9NPN0x50YhH1x"
// 		 "hLsPuWGsUSklZt58jaTfF4ZEQpyUNGc0dqbpBYYBaHHrsX."
// 	 },
//  };

//  #define ntests2 (sizeof (tests2) / sizeof (tests2[0]))

//  int
//  main(void)
//  {
// 	 SHA512_CTX ctx;
// 	 uint8_t sum[64];
// 	 int result = 0;
// 	 int i, cnt;

// 	 for (cnt = 0; cnt < (int)ntests; ++cnt) {
// 		 SHA512_Init(&ctx);
// 		 SHA512_Update(&ctx, tests[cnt].input, strlen(tests[cnt].input));
// 		 SHA512_Final(sum, &ctx);
// 		 if (memcmp(tests[cnt].result, sum, 64) != 0) {
// 			 printf("test %d run %d failed\n", cnt, 1);
// 			 result = 1;
// 		 }

// 		 SHA512_Init(&ctx);
// 		 for (i = 0; tests[cnt].input[i] != '\0'; ++i)
// 			 SHA512_Update(&ctx, &tests[cnt].input[i], 1);
// 		 SHA512_Final(sum, &ctx);
// 		 if (memcmp(tests[cnt].result, sum, 64) != 0) {
// 			 printf("test %d run %d failed\n", cnt, 2);
// 			 result = 1;
// 		 }
// 	 }

// 	 /* Test vector from FIPS 180-2: appendix C.3. */
// 	 char buf[1000];

// 	 memset(buf, 'a', sizeof(buf));
// 	 SHA512_Init(&ctx);
// 	 for (i = 0; i < 1000; ++i)
// 		 SHA512_Update(&ctx, buf, sizeof(buf));
// 	 SHA512_Final(sum, &ctx);
// 	 static const char expected[64] =
// 	 "\xe7\x18\x48\x3d\x0c\xe7\x69\x64\x4e\x2e\x42\xc7\xbc\x15\xb4\x63"
// 	 "\x8e\x1f\x98\xb1\x3b\x20\x44\x28\x56\x32\xa8\x03\xaf\xa9\x73\xeb"
// 	 "\xde\x0f\xf2\x44\x87\x7e\xa6\x0a\x4c\xb0\x43\x2c\xe5\x77\xc3\x1b"
// 	 "\xeb\x00\x9c\x5c\x2c\x49\xaa\x2e\x4e\xad\xb2\x17\xad\x8c\xc0\x9b";

// 	 if (memcmp(expected, sum, 64) != 0) {
// 		 printf("test %d failed\n", cnt);
// 		 result = 1;
// 	 }

// 	 for (cnt = 0; cnt < ntests2; ++cnt) {
// 		 char *cp = crypt_sha512(tests2[cnt].input, tests2[cnt].salt);

// 		 if (strcmp(cp, tests2[cnt].expected) != 0) {
// 			 printf("test %d: expected \"%s\", got \"%s\"\n",
// 					cnt, tests2[cnt].expected, cp);
// 			 result = 1;
// 		 }
// 	 }

// 	 if (result == 0)
// 		 puts("all tests OK");

// 	 return result;
//  }

//  #endif /* TEST */
