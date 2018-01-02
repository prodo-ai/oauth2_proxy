package providers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type GitHubProvider struct {
	*ProviderData
	Orgs []string
	Team string
}

func NewGitHubProvider(p *ProviderData) *GitHubProvider {
	p.ProviderName = "GitHub"
	if p.LoginURL == nil || p.LoginURL.String() == "" {
		p.LoginURL = &url.URL{
			Scheme: "https",
			Host:   "github.com",
			Path:   "/login/oauth/authorize",
		}
	}
	if p.RedeemURL == nil || p.RedeemURL.String() == "" {
		p.RedeemURL = &url.URL{
			Scheme: "https",
			Host:   "github.com",
			Path:   "/login/oauth/access_token",
		}
	}
	// ValidationURL is the API Base URL
	if p.ValidateURL == nil || p.ValidateURL.String() == "" {
		p.ValidateURL = &url.URL{
			Scheme: "https",
			Host:   "api.github.com",
			Path:   "/",
		}
	}
	if p.Scope == "" {
		p.Scope = "user:email"
	}
	return &GitHubProvider{ProviderData: p}
}
func (p *GitHubProvider) SetOrgTeam(orgs []string, team string) {
	p.Orgs = orgs
	p.Team = team
	if len(orgs) != 0 || team != "" {
		p.Scope += " read:org"
	}
}

func (p *GitHubProvider) hasOrg(accessToken string) (bool, error) {
	// https://developer.github.com/v3/orgs/#list-your-organizations

	var orgs []struct {
		Login string `json:"login"`
	}

	type orgsPage []struct {
		Login string `json:"login"`
	}

	pn := 1
	for {
		params := url.Values{
			"limit": {"200"},
			"page":  {strconv.Itoa(pn)},
		}

		endpoint := &url.URL{
			Scheme:   p.ValidateURL.Scheme,
			Host:     p.ValidateURL.Host,
			Path:     path.Join(p.ValidateURL.Path, "/user/orgs"),
			RawQuery: params.Encode(),
		}
		req, _ := http.NewRequest("GET", endpoint.String(), nil)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		req.Header.Set("Authorization", fmt.Sprintf("token %s", accessToken))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return false, err
		}
		if resp.StatusCode != 200 {
			return false, fmt.Errorf(
				"got %d from %q %s", resp.StatusCode, endpoint.String(), body)
		}

		var op orgsPage
		if err := json.Unmarshal(body, &op); err != nil {
			return false, err
		}
		if len(op) == 0 {
			break
		}

		orgs = append(orgs, op...)
		pn += 1
	}

	var presentOrgs []string

	for _, githubOrg := range p.Orgs {
		for _, org := range orgs {
			if githubOrg == org.Login {
				presentOrgs = append(presentOrgs, org.Login)
			}
		}
	}

	if len(presentOrgs) > 0 {
		log.Printf("Found Github Organizations: %v", presentOrgs)
		return true, nil
	}

	log.Printf("Missing Organizations:%v in %v", p.Orgs, presentOrgs)
	return false, nil
}

func (p *GitHubProvider) hasOrgAndTeam(accessToken string) (bool, error) {
	// https://developer.github.com/v3/orgs/teams/#list-user-teams

	var teams []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Org  struct {
			Login string `json:"login"`
		} `json:"organization"`
	}

	params := url.Values{
		"limit": {"200"},
	}

	endpoint := &url.URL{
		Scheme:   p.ValidateURL.Scheme,
		Host:     p.ValidateURL.Host,
		Path:     path.Join(p.ValidateURL.Path, "/user/teams"),
		RawQuery: params.Encode(),
	}
	req, _ := http.NewRequest("GET", endpoint.String(), nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", accessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return false, err
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf(
			"got %d from %q %s", resp.StatusCode, endpoint.String(), body)
	}

	if err := json.Unmarshal(body, &teams); err != nil {
		return false, fmt.Errorf("%s unmarshaling %s", err, body)
	}

	var hasOrg bool
	presentOrgs := make(map[string]bool)
	var presentTeams []string
	for _, team := range teams {
		presentOrgs[team.Org.Login] = true
		for _, org := range p.Orgs {
			if org == team.Org.Login {
				hasOrg = true
				ts := strings.Split(p.Team, ",")
				for _, t := range ts {
					if t == team.Slug {
						log.Printf("Found Github Organization:%q Team:%q (Name:%q)", team.Org.Login, team.Slug, team.Name)
						return true, nil
					}
				}
				presentTeams = append(presentTeams, team.Slug)
			}
		}
	}

	if hasOrg {
		log.Printf("Missing Team:%q from Orgs:%v in teams: %v", p.Team, p.Orgs, presentTeams)
	} else {
		var allOrgs []string
		for org, _ := range presentOrgs {
			allOrgs = append(allOrgs, org)
		}
		log.Printf("Missing Organizations:%v in %#v", p.Orgs, allOrgs)
	}
	return false, nil
}

func (p *GitHubProvider) GetEmailAddress(s *SessionState) (string, error) {

	var emails []struct {
		Email   string `json:"email"`
		Primary bool   `json:"primary"`
	}

	// if we require an Org or Team, check that first
	if len(p.Orgs) != 0 {
		if p.Team != "" {
			if ok, err := p.hasOrgAndTeam(s.AccessToken); err != nil || !ok {
				return "", err
			}
		} else {
			if ok, err := p.hasOrg(s.AccessToken); err != nil || !ok {
				return "", err
			}
		}
	}

	endpoint := &url.URL{
		Scheme: p.ValidateURL.Scheme,
		Host:   p.ValidateURL.Host,
		Path:   path.Join(p.ValidateURL.Path, "/user/emails"),
	}
	req, _ := http.NewRequest("GET", endpoint.String(), nil)
	req.Header.Set("Authorization", fmt.Sprintf("token %s", s.AccessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("got %d from %q %s",
			resp.StatusCode, endpoint.String(), body)
	}

	log.Printf("got %d from %q %s", resp.StatusCode, endpoint.String(), body)

	if err := json.Unmarshal(body, &emails); err != nil {
		return "", fmt.Errorf("%s unmarshaling %s", err, body)
	}

	for _, email := range emails {
		if email.Primary {
			return email.Email, nil
		}
	}

	return "", nil
}

func (p *GitHubProvider) GetUserName(s *SessionState) (string, error) {
	var user struct {
		Login string `json:"login"`
		Email string `json:"email"`
	}

	endpoint := &url.URL{
		Scheme: p.ValidateURL.Scheme,
		Host:   p.ValidateURL.Host,
		Path:   path.Join(p.ValidateURL.Path, "/user"),
	}

	req, err := http.NewRequest("GET", endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("could not create new GET request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", s.AccessToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("got %d from %q %s",
			resp.StatusCode, endpoint.String(), body)
	}

	log.Printf("got %d from %q %s", resp.StatusCode, endpoint.String(), body)

	if err := json.Unmarshal(body, &user); err != nil {
		return "", fmt.Errorf("%s unmarshaling %s", err, body)
	}

	return user.Login, nil
}
