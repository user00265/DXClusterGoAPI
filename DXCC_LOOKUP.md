# DXCC Lookup Implementation Guide

## Overview

DXCC (DX Century Club) entity determination from portable callsigns is complex due to diverse international regulatory conventions. This document outlines the correct parsing logic to handle callsigns in both standard and non-standard formats.

## The Golden Rule

**For DXCC purposes, the entity is determined by the PREFIX component that indicates the location of the transmitter, not the operator's home callsign.**

## Key Concepts

### Portable Callsign Components

A portable callsign has the structure: `[PREFIX]/[CALLSIGN]/[DESIGNATOR]` or variations.

- **Prefix**: 1-3 character country/region code (e.g., `DL`, `HR9`, `KP4`)
- **Callsign**: 3-6 character home callsign (e.g., `K6VHF`, `G0AAA`)
- **Designator**: Portable/mode indicator (e.g., `/P`, `/M`, `/QRP`, `/1`-`/9`)

### Portable Operation Formats

#### International/CEPT Standard (Prefix-First)

```
PREFIX/CALLSIGN
```

- **Format**: `HR9/K6VHF`, `DL/G0AAA`, `KP4/N5ZO`
- **DXCC Entity**: Determined by PREFIX
- **Example**: `HR9/K6VHF` → Look up `HR9` → Honduras

#### Domestic/Regional Operation (Suffix-First)

```
CALLSIGN/DESIGNATOR
```

- **Format**: `K6VHF/4`, `G0AAA/P`, `KF0ACN/QRP`
- **Designators**: `/P`, `/M`, `/MM`, `/AM`, `/QRP`, `/LGT`, `/0`-`/9` (CQ zones)
- **DXCC Entity**: Determined by CALLSIGN (designator is stripped)
- **Example**: `KF0ACN/1` → Strip `/1` → Look up `KF0ACN` → USA

#### Non-Standard/Flipped Format (Suffix-First Country Code)

```
CALLSIGN/PREFIX
```

- **Format**: `K6VHF/HR9`
- **Status**: Non-conventional but may be required by some countries
- **DXCC Entity**: Try PREFIX first; if valid country code found, use it; else fall back to CALLSIGN
- **Example**: `K6VHF/HR9` → Check if `HR9` is valid prefix → Honduras (if found); else USA

#### Complex/Nested Format

```
PREFIX/CALLSIGN/DESIGNATOR
```

- **Format**: `KP4/KF0ACN/QRP`, `DL/K6VHF/P`
- **Parsing**: Strip designators from right first, then apply international/domestic rules
- **Example**: `KP4/KF0ACN/QRP` → Strip `/QRP` → `KP4/KF0ACN` → Look up `KP4` → Puerto Rico

#### US-Canada Bilateral Exception

```
US_CALLSIGN/VE#  or  VE_CALLSIGN/W#
```

- **Format**: `K6VHF/VE3`, `VE3AB/W6`
- **DXCC Entity**: Determined by the SUFFIX PREFIX (not the home callsign)
- **Example**: `K6VHF/VE3` → Look up `VE3` → Canada

## Implementation Algorithm

### Step 1: Strip Designators from the Right

Remove known domestic/mode designators from the right side of the callsign:

- Single letters: `/P`, `/M`, `/A`, `/B`, `/R`, `/X`, `/D`, `/T`
- Maritime: `/MM`, `/AM`
- Low-power/special: `/QRP`, `/LGT`
- CQ zones: `/0`, `/1`-`/9`

**Example**: `KP4/KF0ACN/QRP` → Strip `/QRP` → `KP4/KF0ACN`

### Step 2: Analyze Slash Structure

After stripping designators, examine the remaining callsign:

1. **No slashes remaining**: Use the whole callsign for lookup

   - Example: `K6VHF` → Look up `K6VHF`

2. **One slash**: Split into `before` and `after`

   - Proceed to Step 3

3. **Multiple slashes**: This is ambiguous/malformed. Recommend logging and using the first prefix component
   - Example: `A/B/C` → Try to resolve as international if A looks like prefix, else use B

### Step 3: Determine Lookup Strategy

Given `before` and `after` (from splitting on the first slash):

#### Case 3a: Check for International Format (Prefix-First)

```
Is `before` a valid country prefix?
  AND
Is `after` a valid callsign format?
```

- **Yes**: Use `before` for DXCC lookup (international format)
- **No**: Continue to Case 3b

**Country Prefix Pattern**: 1-3 alphanumeric characters, typically `[A-Z][0-9A-Z]{0,2}`

**Callsign Pattern**: 3+ characters, typically `[A-Z0-9]{3,}` (letters and digits only)

#### Case 3b: Check for Domestic Format (Suffix-First)

```
Is `before` a valid callsign?
  AND
Is `after` a domestic designator we haven't yet stripped?
```

- **Yes**: Use `before` for DXCC lookup (domestic format)
- **No**: Continue to Case 3c

**Known Designators** (shouldn't reach here if Step 1 worked, but safety check):

- `/P`, `/M`, `/MM`, `/AM`, `/QRP`, `/LGT`
- `/0`-`/9` (CQ zones)

#### Case 3c: Check for US-Canada Bilateral Exception

```
Is `before` a US callsign (W, K, N, A prefix)?
  AND
Is `after` a Canadian prefix (VE followed by optional digit)?
```

OR

```
Is `before` a Canadian callsign (VE prefix)?
  AND
Is `after` a US prefix (W, K, N, A followed by optional digit)?
```

- **Yes**: Use `after` for DXCC lookup
- **No**: Continue to Case 3d

#### Case 3d: Non-Standard Suffix-First Country Code (Fallback)

```
Is `after` a valid country prefix?
```

- **Yes**: Use `after` for DXCC lookup (non-standard but possibly required format)
- **No**: Use `before` for DXCC lookup (fallback to home callsign)

### Step 4: Progressive Prefix Matching

Once you have the lookup key (prefix or callsign):

1. Try exact match in the prefixes/exceptions database
2. If no match, progressively shorten from the right and retry
3. If still no match, check entity-based fallbacks
4. Return DXCC entity or "NONE" if no match found

## Examples

### Example 1: International Format

```
Input: HR9/K6VHF
Step 1: No designators to strip → HR9/K6VHF
Step 2: One slash found → before=HR9, after=K6VHF
Step 3a: HR9 is 3 chars (country prefix), K6VHF is 5 chars (callsign) → YES
Result: Look up HR9 → Honduras ✓
```

### Example 2: Domestic Format

```
Input: KF0ACN/1
Step 1: Strip /1 (CQ zone) → KF0ACN
Step 2: No slashes remaining
Result: Look up KF0ACN → USA ✓
```

### Example 3: Non-Standard Suffix-First

```
Input: K6VHF/HR9
Step 1: No designators to strip → K6VHF/HR9
Step 2: One slash found → before=K6VHF, after=HR9
Step 3a: K6VHF is 5 chars (callsign), HR9 is 3 chars (prefix) → NO
Step 3b: K6VHF is valid callsign, HR9 is not domestic designator → NO
Step 3c: K6VHF is US call, HR9 is not VE/W prefix → NO
Step 3d: HR9 is valid country prefix → YES
Result: Look up HR9 → Honduras ✓
```

### Example 4: Complex Nested Format

```
Input: KP4/KF0ACN/QRP
Step 1: Strip /QRP → KP4/KF0ACN
Step 2: One slash found → before=KP4, after=KF0ACN
Step 3a: KP4 is 3 chars (country prefix), KF0ACN is 6 chars (callsign) → YES
Result: Look up KP4 → Puerto Rico ✓
```

### Example 5: US-Canada Exception

```
Input: K6VHF/VE3
Step 1: No designators to strip → K6VHF/VE3
Step 2: One slash found → before=K6VHF, after=VE3
Step 3a: K6VHF is callsign, VE3 is prefix → NO
Step 3b: VE3 is not domestic designator → NO
Step 3c: K6VHF is US call (K prefix), VE3 is Canadian prefix → YES
Result: Look up VE3 → Canada ✓
```

## Code Structure

### Helper Functions Needed

1. **`stripDesignatorsFromRight(call string) string`**

   - Removes `/P`, `/M`, `/MM`, `/AM`, `/QRP`, `/LGT`, `/0`-`/9` from the right
   - Iteratively checks and removes until no more designators found

2. **`isCountryPrefix(prefix string) bool`**

   - Pattern: 1-3 alphanumeric characters
   - More strict check: Verify against known DXCC prefixes in database if available

3. **`isCallsignFormat(call string) bool`**

   - Pattern: 3+ alphanumeric characters
   - Can contain digits but not primarily digits

4. **`isUSCallsign(call string) bool`**

   - Prefix check: W, K, N, A (optionally followed by digit)

5. **`isCanadianPrefix(prefix string) bool`**

   - Prefix check: VE (optionally followed by digit)

6. **`determineLookupKey(call string) string`**
   - Main algorithm that implements Steps 1-4
   - Returns the prefix or callsign to use for DXCC lookup

## Testing Recommendations

## Real-World Test Callsigns and Expected DXCC Entities

The following table provides real-world callsigns and their expected DXCC entity results, as used in `utils/dxcc_lookup_test.go`. Use these for validation and as onboarding examples for LLMs or new developers.

| Callsign  | Expected DXCC Entity     |
| --------- | ------------------------ |
| K1JT      | UNITED STATES OF AMERICA |
| W2/JR1AQN | UNITED STATES OF AMERICA |
| KF0ACN    | UNITED STATES OF AMERICA |
| XE1YO     | MEXICO                   |
| WP4NVX    | PUERTO RICO              |
| YY5YDT    | VENEZUELA                |
| ZW5B      | BRAZIL                   |
| VA3WR     | CANADA                   |
| PI4DX     | NETHERLANDS              |
| TI4LAS    | COSTA RICA               |
| CO8LY     | CUBA                     |
| JA7QVI    | JAPAN                    |
| CT1BFP    | PORTUGAL                 |
| F4JRC     | FRANCE                   |
| HI3R      | DOMINICAN REPUBLIC       |
| K2J       | UNITED STATES OF AMERICA |
| DJ7NT     | GERMANY                  |
| DB4SCW    | GERMANY                  |
| HB9HIL    | SWITZERLAND              |
| W9/N3YPL  | UNITED STATES OF AMERICA |
| AB4KK     | UNITED STATES OF AMERICA |
| VE3HZ     | CANADA                   |
| NP3DM     | PUERTO RICO              |
| KK7UIL/AE | UNITED STATES OF AMERICA |
| R4FBH     | EUROPEAN RUSSIA          |
| RA3RCL    | EUROPEAN RUSSIA          |
| WH6HI     | HAWAII                   |
| WL7X      | ALASKA                   |
| D4Z       | CAPE VERDE               |
| 8P6EX     | BARBADOS                 |
| FM5KC     | MARTINIQUE               |
| V31MA     | BELIZE                   |
| HP6LEF    | PANAMA                   |
| D2UY      | ANGOLA                   |
| IK4GRO    | ITALY                    |
| EA4GOY    | SPAIN                    |
| W1AW      | UNITED STATES OF AMERICA |
| MI0NWA    | NORTHERN IRELAND         |
| M0MCX     | ENGLAND                  |
| GM0OPS    | SCOTLAND                 |
| FK8GX     | NEW CALEDONIA            |
| ZL2CA     | NEW ZEALAND              |
| K6VHF/HR9 | HONDURAS                 |

These cases cover a wide range of international, domestic, and special DXCC scenarios. Use them to validate your implementation and as a quick reference for onboarding.

## DXCC Data Sources and Recommendations

To implement robust DXCC entity lookup, use the following data sources:

- **ARRL dxcclist.txt**

  - Official DXCC entity list (https://www.arrl.org/files/file/dxcclist.txt)
  - Contains entity names and codes, but not prefix-to-entity mappings.

- **AD1C Country Files (cty.dat)**

  - The gold standard for callsign-to-DXCC mapping.
  - Download: https://www.country-files.com/cty/download/3534/cty-3534.zip (latest as of writing)
  - Format documentation: https://www.country-files.com/cty-dat-format/
  - No API for versioning; must manually check for updates.

- **Wavelog cty.xml**

  - Open-source XML version, used as fallback in code.
  - Download: https://github.com/wavelog/dxcc_data/raw/refs/heads/master/cty.xml.gz

- **Club Log API**
  - Provides DXCC data via API, used in backend/dxcc/client.go.
  - Subject to rate limits and availability.

**Recommendation:**

- Prefer AD1C cty.dat for prefix/entity mapping (most complete, widely accepted).
- Use Wavelog’s cty.xml for open-source and automated updates.
- Use Club Log API only if no other source is available, due to API limitations.

For future LLMs and developers: Always check for the latest version of cty.dat manually, and consider automating downloads from open-source XML sources when possible.

---

### Useful Links

- ARRL DXCC Rules: https://www.arrl.org/dxcc
- ARRL dxcclist.txt: https://www.arrl.org/files/file/dxcclist.txt
- AD1C Country Files: http://www.country-files.com/
- AD1C cty.dat format: https://www.country-files.com/cty-dat-format/
- Wavelog cty.xml: https://github.com/wavelog/dxcc_data/raw/refs/heads/master/cty.xml.gz
- Club Log: https://clublog.org/
- IARU Portable Operations: https://www.iaru.org/
- CEPT T/R 61-01: https://www.cept.org/ecc/topics/cept-licensing/
