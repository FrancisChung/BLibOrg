# Design: Book Organiser - Initial Version

## Problem
I have tons of ebooks that are have various naming conventions. 
It's often hard to look for books at first glance because of these wildly varying filenames

## Goal
I need a book organiser that can rename it and catalog it in the right folders

## Context / constraints
- This will touch both filesystems in linux or windows (NTFS)
- You can refer this popular project for reference, but I do not wish to copy this project verbatim as it has a lot of functionality I won't need
https://github.com/na--/ebook-tools

## Proposed approach
- The solution should work equally in Windows or Linux
- Main UI
    - As a minimum, it should these things
    - View 1: It should a list of all the ebooks in the working directory and how each book is going to be renamed
    - View 2: It should show where the new books is going to be moved to (like a preview)
    - View 3: Things relating to Organiser
- I should be able to configure the following in a config file
    - Filename format: Book, Published Year, Edition, Author etc
    - Catalogue Folder Structure describing categories, subcategories 
    - Working Folder - Where the eBooks to be renamed and moved are located
    - Organiser - Not sure what config items here yet
- Organiser
    - This function should look at a filename and figure out where to move it to

### Key decisions

## Affected files / components
- Examples of books with bad file names
    - Build+Your+API+with+Spring.pdf, Building Resilient Distributed Systems (for dagfhhhhh dfafaf){Sam Newman}(2024, O&_039_Reilly Media, Inc.){115667237} libgen.li.pdf, _OceanofPDF.com_Dissecting_the_Dark_Web_-_Lindsay_Kaye.pdf
    - Note that are many variation of bad file names, and the above 3 examples are only a fraction of the variety.

## Open questions
- Organiser
    - I have no idea how it should work or what algorithms or heuristics out there will help. Perhaps there's something in ebook-tools repo.
    

## Testing / verification
As a minimum, the tests will be needed to ensure:
    - File names are being renamed correctly
    - It is being moved according to the Organiser rules or configuration

## Rollout
- Assume it will just be to my workstations which could be either windows or PC
- The solution should be easily compiled, executed and deployed in either windows or PC

